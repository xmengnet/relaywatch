package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"relaywatch/internal/importer"
	"relaywatch/internal/source"
	"relaywatch/internal/store"
)

// ImportExportHandler nvchecker 导入与 JSON 导出。
type ImportExportHandler struct {
	St       store.Store
	Registry *source.Registry
}

func (h *ImportExportHandler) routes(r chiRouterIface) {
	r.Post("/import/preview", h.requirePerm(h.preview, store.PermImport))
	r.Post("/import", h.requirePerm(h.doImport, store.PermImport))
	r.Get("/export", h.requirePerm(h.exportAll, store.PermExport))
}

// requirePerm 细粒度权限包装：无权限 403。
func (h *ImportExportHandler) requirePerm(next http.HandlerFunc, perm string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authUser(r).Can(perm) {
			forbidPermission(w, map[string]string{
				store.PermImport: "导入",
				store.PermExport: "导出",
			}[perm])
			return
		}
		next(w, r)
	}
}

type importPreviewItem struct {
	Name          string         `json:"name"`
	SourceType    string         `json:"source_type"`
	SourceConfig  map[string]any `json:"source_config"`
	VersionPrefix string         `json:"version_prefix,omitempty"`
	IncludeRegex  string         `json:"include_regex,omitempty"`
	IgnoreRegex   string         `json:"ignore_regex,omitempty"`
	FromPattern   string         `json:"from_pattern,omitempty"`
	ToPattern     string         `json:"to_pattern,omitempty"`
	Valid         bool           `json:"valid"`
	Error         string         `json:"error,omitempty"`
}

// preview POST {text} → 解析结果与警告，不落库。
func (h *ImportExportHandler) preview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	res, err := importer.Parse(req.Text)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items := make([]importPreviewItem, 0, len(res.Items))
	for _, it := range res.Items {
		p := importPreviewItem{
			Name: it.Name, SourceType: it.SourceType, SourceConfig: it.SourceConfig,
			VersionPrefix: it.VersionPrefix, IncludeRegex: it.IncludeRegex, IgnoreRegex: it.IgnoreRegex,
			FromPattern: it.FromPattern, ToPattern: it.ToPattern,
		}
		// 校验源类型存在 + 参数合法
		if src, ok := h.Registry.Get(it.SourceType); ok {
			if err := src.Validate(it.SourceConfig); err != nil {
				p.Valid, p.Error = false, err.Error()
			} else {
				p.Valid = true
			}
		} else {
			p.Valid, p.Error = false, "注册表无此源类型: "+it.SourceType
		}
		items = append(items, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"warnings": res.Warnings,
		"ignored":  res.Ignored,
	})
}

// doImport POST {text, selected: [names]} → 只导入选中的条目。
func (h *ImportExportHandler) doImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text     string   `json:"text"`
		Selected []string `json:"selected"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	res, err := importer.Parse(req.Text)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	want := map[string]bool{}
	for _, n := range req.Selected {
		want[n] = true
	}
	imported, skipped := 0, 0
	for _, it := range res.Items {
		if len(req.Selected) > 0 && !want[it.Name] {
			skipped++
			continue
		}
		src, ok := h.Registry.Get(it.SourceType)
		if !ok {
			skipped++
			continue
		}
		if err := src.Validate(it.SourceConfig); err != nil {
			skipped++
			continue
		}
		cfgJSON, _ := json.Marshal(it.SourceConfig)
		m := &store.Monitor{
			Name:                it.Name,
			SourceType:          it.SourceType,
			SourceConfig:        string(cfgJSON),
			VersionPrefix:       it.VersionPrefix,
			VersionIncludeRegex: it.IncludeRegex,
			VersionIgnoreRegex:  it.IgnoreRegex,
			FromPattern:         it.FromPattern,
			ToPattern:           it.ToPattern,
			IntervalSec:         300,
			Enabled:             true,
			State:               store.StateUnknown,
		}
		if err := h.St.CreateMonitor(r.Context(), m); err != nil {
			skipped++
			continue
		}
		imported++
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported, "skipped": skipped})
}

// ---------- 导出 ----------

type exportMonitor struct {
	Name                string          `json:"name"`
	SourceType          string          `json:"source_type"`
	SourceConfig        json.RawMessage `json:"source_config"`
	VersionPrefix       string          `json:"version_prefix,omitempty"`
	VersionSuffix       string          `json:"version_suffix,omitempty"`
	VersionIncludeRegex string          `json:"version_include_regex,omitempty"`
	VersionIgnoreRegex  string          `json:"version_ignore_regex,omitempty"`
	FromPattern         string          `json:"from_pattern,omitempty"`
	ToPattern           string          `json:"to_pattern,omitempty"`
	IntervalSec         int             `json:"interval_sec"`
	Enabled             bool            `json:"enabled"`
	NotifyCooldownSec   int             `json:"notify_cooldown_sec"`
	Tags                []string        `json:"tags"`
	Note                string          `json:"note,omitempty"`
	ChannelIDs          []int64         `json:"channel_ids,omitempty"`
}

type exportChannel struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Config   json.RawMessage `json:"config"`
	Template string          `json:"template,omitempty"`
	Enabled  bool            `json:"enabled"`
}

// secretKeys 各渠道类型里需要脱敏的键。
var secretKeys = map[string]bool{
	"token": true, "secret": true, "bot_token": true, "password": true,
	"webhook": true, "webhook_url": true, "url_secret": true, "key": true,
}

// exportAll GET /export?redact=1（默认脱敏）。
// 普通用户导出自己的资源；admin 默认全部，?owner=me 仅导出自己的。
func (h *ImportExportHandler) exportAll(w http.ResponseWriter, r *http.Request) {
	redact := r.URL.Query().Get("redact") != "0"

	u := authUser(r)
	filter := store.MonitorFilter{PerPage: 10000}
	if !isAdmin(u) || r.URL.Query().Get("owner") == "me" {
		filter.OwnerID = u.ID
	}
	monitors, _, err := h.St.ListMonitors(r.Context(), filter)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var channels []*store.Channel
	if isAdmin(u) {
		channels, err = h.St.ListChannelsAll(r.Context())
	} else {
		channels, err = h.St.ListChannelsVisible(r.Context(), u.ID)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}

	exportMonitors := make([]exportMonitor, 0, len(monitors))
	for _, m := range monitors {
		cfg := json.RawMessage(m.SourceConfig)
		if redact {
			cfg = redactJSON(m.SourceConfig, func(k, v string) string {
				if strings.EqualFold(k, "token") {
					return "REDACTED"
				}
				return v
			})
		}
		tags := m.Tags
		if tags == nil {
			tags = []string{}
		}
		exportMonitors = append(exportMonitors, exportMonitor{
			Name: m.Name, SourceType: m.SourceType, SourceConfig: cfg,
			VersionPrefix: m.VersionPrefix, VersionSuffix: m.VersionSuffix,
			VersionIncludeRegex: m.VersionIncludeRegex, VersionIgnoreRegex: m.VersionIgnoreRegex,
			FromPattern: m.FromPattern, ToPattern: m.ToPattern,
			IntervalSec: m.IntervalSec, Enabled: m.Enabled,
			NotifyCooldownSec: m.NotifyCooldownSec,
			Tags:              tags, Note: m.Note, ChannelIDs: m.ChannelIDs,
		})
	}

	exportChannels := make([]exportChannel, 0, len(channels))
	for _, c := range channels {
		cfg := json.RawMessage(c.Config)
		if redact {
			cfg = redactJSON(c.Config, func(k, v string) string {
				if secretKeys[strings.ToLower(k)] && v != "" {
					return "REDACTED"
				}
				return v
			})
		}
		exportChannels = append(exportChannels, exportChannel{
			Name: c.Name, Type: c.Type, Config: cfg, Template: c.Template, Enabled: c.Enabled,
		})
	}

	w.Header().Set("Content-Disposition", "attachment; filename=relaywatch-export.json")
	writeJSON(w, http.StatusOK, map[string]any{
		"version":  1,
		"exported": timeNow().Format("2006-01-02T15:04:05Z07:00"),
		"redacted": redact,
		"monitors": exportMonitors,
		"channels": exportChannels,
	})
}

// redactJSON 深度遍历 JSON，按 judge 函数脱敏叶子值。
func redactJSON(raw string, judge func(key, val string) string) json.RawMessage {
	if raw == "" {
		return json.RawMessage(`{}`)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return json.RawMessage(`{}`)
	}
	redactMap(doc, judge)
	out, err := json.Marshal(doc)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}

func redactMap(doc map[string]any, judge func(key, val string) string) {
	for k, v := range doc {
		switch tv := v.(type) {
		case string:
			doc[k] = judge(k, tv)
		case map[string]any:
			redactMap(tv, judge)
		}
	}
}
