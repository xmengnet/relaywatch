package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"relaywatch/internal/compare"
	"relaywatch/internal/source"
	"relaywatch/internal/store"
)

// MonitorHandler 监控 CRUD。
type MonitorHandler struct {
	St        store.Store
	Registry  *source.Registry
	Evaluator *compare.Evaluator
}

func (h *MonitorHandler) routes(r chi.Router) {
	r.Route("/monitors", func(r chi.Router) {
		r.Get("/", h.list)
		r.Post("/", h.create)
		r.Post("/batch", h.batch)
		r.Get("/{id}", h.get)
		r.Patch("/{id}", h.update)
		r.Delete("/{id}", h.remove)
	})
}

// getMonitorForAccess 按 id 取监控并做归属校验：admin 全通，普通用户仅自己的。
// 一律 404 语义（含无权限），避免资源枚举。
func (h *MonitorHandler) getMonitorForAccess(w http.ResponseWriter, r *http.Request) (*store.Monitor, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return nil, false
	}
	m, err := h.St.GetMonitor(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "监控不存在")
		return nil, false
	}
	if err != nil {
		writeInternalError(w, err)
		return nil, false
	}
	if !canAccessMonitor(authUser(r), m) {
		writeError(w, http.StatusNotFound, "监控不存在")
		return nil, false
	}
	return m, true
}

func (h *MonitorHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.MonitorFilter{
		State:        store.MonitorState(q.Get("state")),
		CompareState: q.Get("compare"),
		Tag:          q.Get("tag"),
		Search:       q.Get("q"),
		SortBy:       q.Get("sort"),
		SortOrder:    q.Get("order"),
	}
	// admin 可用 ?owner_id= 按用户筛选；普通用户强制只看自己
	if v := q.Get("owner_id"); v != "" && isAdmin(authUser(r)) {
		f.OwnerID, _ = strconv.ParseInt(v, 10, 64)
	} else {
		f.OwnerID = scopeOwnerID(r)
	}
	f.Page, f.PerPage = pageParams(r)
	items, total, err := h.St.ListMonitors(r.Context(), f)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if items == nil {
		items = []*store.Monitor{}
	}
	out := make([]*monitorResponse, 0, len(items))
	for _, m := range items {
		resp := toMonitorResponse(m)
		if m.ReferenceMonitorID != nil {
			resp.ReferenceMonitorName = refMonitorName(h.St, r.Context(), *m.ReferenceMonitorID)
		}
		// admin 列表附 owner 名，用于「所有者」列
		if m.OwnerID > 0 && isAdmin(authUser(r)) {
			if u, err := h.St.UserByID(r.Context(), m.OwnerID); err == nil {
				resp.OwnerName = u.Username
			}
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

// refMonitorName 查参照监控名称；已删除时返回 "#id" 降级显示。
func refMonitorName(st store.Store, ctx context.Context, refID int64) string {
	m, err := st.GetMonitor(ctx, refID)
	if err != nil || m == nil || m.Name == "" {
		return fmt.Sprintf("#%d", refID)
	}
	return m.Name
}

type monitorPayload struct {
	Name                string          `json:"name"`
	SourceType          string          `json:"source_type"`
	SourceConfig        json.RawMessage `json:"source_config"`
	VersionPrefix       string          `json:"version_prefix"`
	VersionSuffix       string          `json:"version_suffix"`
	VersionIncludeRegex string          `json:"version_include_regex"`
	VersionIgnoreRegex  string          `json:"version_ignore_regex"`
	FromPattern         string          `json:"from_pattern"`
	ToPattern           string          `json:"to_pattern"`
	IntervalSec         int             `json:"interval_sec"`
	Enabled             *bool           `json:"enabled"`
	NotifyCooldownSec   int             `json:"notify_cooldown_sec"`
	Tags                []string        `json:"tags"`
	Note                string          `json:"note"`
	ChannelIDs          []int64         `json:"channel_ids"`
	ReferenceMonitorID  *int64          `json:"reference_monitor_id"`
	// 手动改基线用于触发更新测试（需求验收 5）
	CurrentVersion *string `json:"current_version"`
	State          *string `json:"state"`
}

func (p *monitorPayload) validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("名称不能为空")
	}
	if p.SourceType == "" {
		return errors.New("source_type 不能为空")
	}
	if p.IntervalSec < 0 {
		return errors.New("interval_sec 不能为负")
	}
	return nil
}

// validateReference 校验参照绑定；另校验参照归属——普通用户只能引用同 owner 的参照。
func (h *MonitorHandler) validateReference(ctx context.Context, selfID, selfOwner int64, refID *int64) error {
	if refID == nil {
		return nil
	}
	if selfID > 0 && *refID == selfID {
		return errors.New("不能将自身设为参照监控")
	}
	ref, err := h.St.GetMonitor(ctx, *refID)
	if errors.Is(err, store.ErrNotFound) || ref == nil {
		return errors.New("参照监控不存在")
	}
	if err != nil {
		return err
	}
	u := authUserFromCtx(ctx)
	if u != nil && u.Role != "admin" && ref.OwnerID != selfOwner {
		return errors.New("参照监控不存在") // 归属不符按不存在处理，防枚举
	}
	if ref.ReferenceMonitorID != nil {
		return errors.New("参照监控不能已有上游参照（仅支持单层参照）")
	}
	if selfID > 0 {
		followers, err := h.St.ListFollowerMonitors(ctx, selfID)
		if err == nil && len(followers) > 0 {
			return errors.New("当前监控已被其他监控设为参照，不能再设置上游参照")
		}
	}
	return nil
}

func (h *MonitorHandler) create(w http.ResponseWriter, r *http.Request) {
	var p monitorPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateSourceConfig(&p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u := authUser(r)
	if err := h.validateReference(r.Context(), 0, u.ID, p.ReferenceMonitorID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !u.Can(store.PermCreateMonitor) {
		forbidPermission(w, "创建监控")
		return
	}
	if p.IntervalSec == 0 {
		p.IntervalSec = 300 // 全局默认接配置后调整
	}
	m := payloadToMonitor(&p)
	m.State = store.StateUnknown
	m.OwnerID = u.ID // 创建归属强制当前用户，不接受请求体传入
	if err := h.St.CreateMonitor(r.Context(), m); err != nil {
		writeInternalError(w, err)
		return
	}
	if m.ReferenceMonitorID != nil && h.Evaluator != nil {
		_ = h.Evaluator.EvaluatePair(r.Context(), m.ID)
	}
	out, err := h.St.GetMonitor(r.Context(), m.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMonitorResponse(out))
}

func (h *MonitorHandler) get(w http.ResponseWriter, r *http.Request) {
	m, ok := h.getMonitorForAccess(w, r)
	if !ok {
		return
	}
	resp := toMonitorResponse(m)
	if m.ReferenceMonitorID != nil {
		resp.ReferenceMonitorName = refMonitorName(h.St, r.Context(), *m.ReferenceMonitorID)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *MonitorHandler) update(w http.ResponseWriter, r *http.Request) {
	m, ok := h.getMonitorForAccess(w, r)
	if !ok {
		return
	}
	if !requireWrite(w, r) {
		return
	}
	id := m.ID
	var p monitorPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateSourceConfig(&p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateReference(r.Context(), id, m.OwnerID, p.ReferenceMonitorID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	applyPayload(m, &p)
	if err := h.St.UpdateMonitor(r.Context(), m); err != nil {
		writeInternalError(w, err)
		return
	}
	if m.ReferenceMonitorID != nil && h.Evaluator != nil {
		_ = h.Evaluator.EvaluatePair(r.Context(), id)
	}
	out, err := h.St.GetMonitor(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMonitorResponse(out))
}

func (h *MonitorHandler) remove(w http.ResponseWriter, r *http.Request) {
	m, ok := h.getMonitorForAccess(w, r)
	if !ok {
		return
	}
	if !requireWrite(w, r) {
		return
	}
	id := m.ID
	_ = h.St.ClearFollowerReferences(r.Context(), id)
	if err := h.St.DeleteMonitor(r.Context(), id); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *MonitorHandler) batch(w http.ResponseWriter, r *http.Request) {
	if !requireWrite(w, r) {
		return
	}
	var req struct {
		Action string  `json:"action"`
		IDs    []int64 `json:"ids"`
		Fields *struct {
			Enabled *bool `json:"enabled"`
		} `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids 不能为空")
		return
	}
	u := authUser(r)
	isAdminUser := isAdmin(u)
	var affected int64
	for _, id := range req.IDs {
		m, err := h.St.GetMonitor(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			writeInternalError(w, err)
			return
		}
		// 普通用户批量操作只作用于自己的资源，他人资源静默跳过
		if !isAdminUser && m.OwnerID != u.ID {
			continue
		}
		switch req.Action {
		case "enable":
			if req.Fields != nil && req.Fields.Enabled != nil {
				m.Enabled = *req.Fields.Enabled
			} else {
				m.Enabled = true
			}
		case "disable":
			m.Enabled = false
			// 停用作废该监控的待投递记录（需求 3.3）
			if _, err := h.St.CancelOpenDeliveries(r.Context(), id, "monitor disabled"); err != nil {
				writeInternalError(w, err)
				return
			}
		case "delete":
			if err := h.St.DeleteMonitor(r.Context(), id); err == nil {
				affected++
			}
			continue
		default:
			writeError(w, http.StatusBadRequest, "不支持的 action: "+req.Action)
			return
		}
		if err := h.St.UpdateMonitor(r.Context(), m); err == nil {
			affected++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"affected": affected})
}

// ---------- 转换工具 ----------

// validateSourceConfig 校验源类型存在、源参数合法、版本过滤正则可编译。
func (h *MonitorHandler) validateSourceConfig(p *monitorPayload) error {
	if h.Registry != nil {
		src, ok := h.Registry.Get(p.SourceType)
		if !ok {
			return errors.New("未知源类型: " + p.SourceType)
		}
		var cfg map[string]any
		if len(p.SourceConfig) > 0 {
			if err := json.Unmarshal(p.SourceConfig, &cfg); err != nil {
				return errors.New("source_config 不是合法 JSON 对象")
			}
		} else {
			cfg = map[string]any{}
		}
		if err := src.Validate(cfg); err != nil {
			return err
		}
	}
	if err := source.ValidateVersionFilters(p.VersionIncludeRegex, p.VersionIgnoreRegex); err != nil {
		return err
	}
	if p.FromPattern != "" {
		if _, err := source.RewriteVersion("", p.FromPattern, p.ToPattern); err != nil {
			return err
		}
	}
	return nil
}

func payloadToMonitor(p *monitorPayload) *store.Monitor {
	m := &store.Monitor{
		Name:                p.Name,
		SourceType:          p.SourceType,
		SourceConfig:        string(p.SourceConfig),
		VersionPrefix:       p.VersionPrefix,
		VersionSuffix:       p.VersionSuffix,
		VersionIncludeRegex: p.VersionIncludeRegex,
		VersionIgnoreRegex:  p.VersionIgnoreRegex,
		FromPattern:         p.FromPattern,
		ToPattern:           p.ToPattern,
		IntervalSec:         p.IntervalSec,
		Enabled:             true,
		NotifyCooldownSec:   p.NotifyCooldownSec,
		Tags:                p.Tags,
		Note:                p.Note,
		ChannelIDs:          p.ChannelIDs,
		ReferenceMonitorID:  p.ReferenceMonitorID,
	}
	if p.Enabled != nil {
		m.Enabled = *p.Enabled
	}
	if p.CurrentVersion != nil {
		m.CurrentVersion = *p.CurrentVersion
	}
	return m
}

func applyPayload(m *store.Monitor, p *monitorPayload) {
	m.Name = p.Name
	m.SourceType = p.SourceType
	if len(p.SourceConfig) > 0 {
		m.SourceConfig = string(p.SourceConfig)
	}
	m.VersionPrefix = p.VersionPrefix
	m.VersionSuffix = p.VersionSuffix
	m.VersionIncludeRegex = p.VersionIncludeRegex
	m.VersionIgnoreRegex = p.VersionIgnoreRegex
	m.FromPattern = p.FromPattern
	m.ToPattern = p.ToPattern
	m.ReferenceMonitorID = p.ReferenceMonitorID
	if p.IntervalSec > 0 {
		m.IntervalSec = p.IntervalSec
	}
	if p.Enabled != nil {
		m.Enabled = *p.Enabled
	}
	m.NotifyCooldownSec = p.NotifyCooldownSec
	if p.Tags != nil {
		m.Tags = p.Tags
	}
	m.Note = p.Note
	if p.ChannelIDs != nil {
		m.ChannelIDs = p.ChannelIDs
	}
	if p.CurrentVersion != nil {
		m.CurrentVersion = *p.CurrentVersion
	}
	if p.State != nil {
		m.State = store.MonitorState(*p.State)
	}
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}
