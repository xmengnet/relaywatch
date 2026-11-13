package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"relaywatch/internal/notifier"
	"relaywatch/internal/store"
)

// ChannelHandler 通知渠道 CRUD、测试发送。
type ChannelHandler struct {
	St        store.Store
	Notifiers *notifier.Registry
	// TestSender 由 main 注入（独立于 deliveries 的直接发送通道）。
	TestSender func(ctx context.Context, ch *store.Channel) error
}

func (h *ChannelHandler) routes(r chiRouterIface) {
	r.Get("/notifiers/types", h.notifierTypes)
	r.Get("/channels", h.list)
	r.Post("/channels", h.create)
	r.Post("/channels/{id}/test", h.test)
	r.Get("/channels/{id}", h.get)
	r.Patch("/channels/{id}", h.update)
	r.Delete("/channels/{id}", h.remove)
}

func (h *ChannelHandler) notifierTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": h.Notifiers.Metas()})
}

type channelPayload struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Config   json.RawMessage `json:"config"`
	Template string          `json:"template"`
	Enabled  *bool           `json:"enabled"`
	Shared   *bool           `json:"shared"` // 仅 admin：设为全局共享渠道
}

func (p *channelPayload) validate() error {
	if p.Name == "" {
		return errors.New("名称不能为空")
	}
	if p.Type == "" {
		return errors.New("type 不能为空")
	}
	return nil
}

func (h *ChannelHandler) create(w http.ResponseWriter, r *http.Request) {
	var p channelPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateChannelType(h.Notifiers, &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u := authUser(r)
	if !u.Can(store.PermCreateChannel) {
		forbidPermission(w, "创建渠道")
		return
	}
	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	c := &store.Channel{
		Name:     p.Name,
		Type:     p.Type,
		Config:   string(p.Config),
		Template: p.Template,
		Enabled:  enabled,
	}
	if isAdmin(u) {
		if p.Shared != nil && *p.Shared {
			c.OwnerID = nil // 全局共享
		} else {
			c.OwnerID = &u.ID
		}
	} else {
		c.OwnerID = &u.ID // 普通用户不可建共享渠道
	}
	if err := h.St.CreateChannel(r.Context(), c); err != nil {
		writeInternalError(w, err)
		return
	}
	out, _ := h.St.GetChannel(r.Context(), c.ID)
	writeJSON(w, http.StatusCreated, h.view(out, authUser(r)))
}

func validateChannelType(reg *notifier.Registry, p *channelPayload) error {
	n, ok := reg.Get(p.Type)
	if !ok {
		return errors.New("未知渠道类型: " + p.Type)
	}
	var cfg map[string]any
	if len(p.Config) > 0 {
		if err := json.Unmarshal(p.Config, &cfg); err != nil {
			return errors.New("config 不是合法 JSON 对象")
		}
	} else {
		cfg = map[string]any{}
	}
	return n.Validate(cfg)
}

func (h *ChannelHandler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := h.St.GetChannel(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "渠道不存在")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !canAccessChannel(authUser(r), c) {
		writeError(w, http.StatusNotFound, "渠道不存在") // 404 语义，防枚举
		return
	}
	writeJSON(w, http.StatusOK, h.view(c, authUser(r)))
}

func (h *ChannelHandler) list(w http.ResponseWriter, r *http.Request) {
	u := authUser(r)
	var channels []*store.Channel
	var err error
	if isAdmin(u) {
		channels, err = h.St.ListChannelsAll(r.Context())
	} else {
		channels, err = h.St.ListChannelsVisible(r.Context(), u.ID)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]*channelResponse, 0, len(channels))
	for _, c := range channels {
		out = append(out, h.view(c, u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// view 渠道 API 视图：无修改权（非 owner 且非 admin）时掩掉 config 里的机密字段，
// 共享渠道的 bot_token/密码等不下发给普通用户。
func (h *ChannelHandler) view(c *store.Channel, u *store.User) *channelResponse {
	resp := toChannelResponse(c)
	if !canModifyChannel(u, c) {
		resp.Config = maskedConfig(h.Notifiers, c.Type, c.Config)
	}
	return resp
}

// channelMask 非法/未知类型兜底用：与导出脱敏共用一套键名。
var channelMask = "••••••"

// maskedConfig 按 notifier 元数据把 Type=secret 的字段替换为掩码；
// 元数据缺失时退回导出用的静态键名表。
func maskedConfig(reg *notifier.Registry, cfgType, config string) string {
	secretKeys := map[string]bool{}
	if n, ok := reg.Get(cfgType); ok {
		if mp, isMeta := n.(notifier.MetaProvider); isMeta {
			for _, f := range mp.Meta().Fields {
				if f.Type == "secret" {
					secretKeys[strings.ToLower(f.Key)] = true
				}
			}
		}
	}
	if len(secretKeys) == 0 {
		secretKeys = secretKeysByType(cfgType)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(config), &doc); err != nil {
		return config
	}
	hit := false
	for k, v := range doc {
		if secretKeys[strings.ToLower(k)] {
			if s, ok := v.(string); ok && s != "" {
				doc[k] = channelMask
				hit = true
			}
		}
	}
	if !hit {
		return config
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return config
	}
	return string(out)
}

// secretKeysByType 静态兜底：notifier 元数据不可用时的脱敏键名（与导出一致）。
func secretKeysByType(cfgType string) map[string]bool {
	_ = cfgType
	return map[string]bool{
		"token": true, "secret": true, "bot_token": true, "password": true,
		"webhook": true, "webhook_url": true, "url_secret": true, "key": true,
		"sendkey": true, "app_token": true,
	}
}

func (h *ChannelHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := h.St.GetChannel(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "渠道不存在")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	u := authUser(r)
	if !canModifyChannel(u, c) {
		writeError(w, http.StatusNotFound, "渠道不存在")
		return
	}
	if !requireWrite(w, r) {
		return
	}
	var p channelPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateChannelType(h.Notifiers, &p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c.Name = p.Name
	c.Type = p.Type
	if len(p.Config) > 0 {
		c.Config = string(p.Config)
	}
	c.Template = p.Template
	if p.Enabled != nil {
		c.Enabled = *p.Enabled
	}
	// 共享开关仅 admin 可改；admin 显式传 shared 时更新归属
	if isAdmin(u) && p.Shared != nil {
		if *p.Shared {
			c.OwnerID = nil
		} else if c.OwnerID == nil {
			c.OwnerID = &u.ID
		}
	}
	if err := h.St.UpdateChannel(r.Context(), c); err != nil {
		writeInternalError(w, err)
		return
	}
	out, _ := h.St.GetChannel(r.Context(), id)
	writeJSON(w, http.StatusOK, h.view(out, authUser(r)))
}

func (h *ChannelHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := h.St.GetChannel(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !canModifyChannel(authUser(r), c) {
		writeError(w, http.StatusNotFound, "渠道不存在")
		return
	}
	if !requireWrite(w, r) {
		return
	}
	if n, err := h.St.ChannelUsageCount(r.Context(), id); err == nil && n > 0 {
		writeError(w, http.StatusConflict, "该渠道被 "+strconv.Itoa(n)+" 条监控使用，请先解除绑定")
		return
	}
	if err := h.St.DeleteChannel(r.Context(), id); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ChannelHandler) test(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := h.St.GetChannel(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "渠道不存在")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !canAccessChannel(authUser(r), c) {
		writeError(w, http.StatusNotFound, "渠道不存在")
		return
	}
	if !requireWrite(w, r) {
		return
	}
	if h.TestSender == nil {
		writeError(w, http.StatusInternalServerError, "测试发送未配置")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := h.TestSender(ctx, c); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
