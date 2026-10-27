// Package source 定义监控源接口与各类型实现。
package source

import (
	"context"
	"time"
)

// Result 是一次版本查询的结果。
type Result struct {
	Version     string         // 原始版本字符串，如 "v0.1.19"
	URL         string         // 上游页面链接，用于通知
	PublishedAt time.Time      // 可选：上游发布时间
	Extra       map[string]any // 可选：附加信息
}

// NotFoundError 表示上游明确不存在（仓库删除、包下架），
// Checker 据此进入 missing 状态而非 error。
type NotFoundError struct {
	Msg string
}

func (e *NotFoundError) Error() string {
	if e.Msg == "" {
		return "not found"
	}
	return e.Msg
}

// Source 是所有监控源必须实现的接口。
type Source interface {
	// Type 返回源类型标识，与 monitors.source_type 一致。
	Type() string
	// Validate 校验配置是否合法，在保存监控记录时调用。
	Validate(cfg map[string]any) error
	// Fetch 查询当前版本。
	Fetch(ctx context.Context, cfg map[string]any) (*Result, error)
}

// FieldSpec 描述源的一个配置字段，供前端动态渲染表单。
type FieldSpec struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // string | bool | int | select | secret
	Required    bool     `json:"required"`
	Default     any      `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
}

// Meta 源类型的元数据。
type Meta struct {
	Type   string      `json:"type"`
	Label  string      `json:"label"`
	Icon   string      `json:"icon,omitempty"`
	Fields []FieldSpec `json:"fields"`
}

// MetaProvider 是能自我描述的源（全部内置源都实现）。
type MetaProvider interface {
	Meta() Meta
}

// Registry 是源类型的注册表。
type Registry struct {
	sources map[string]Source
	order   []string // 保持注册顺序供列表展示
}

func NewRegistry() *Registry {
	return &Registry{sources: map[string]Source{}}
}

// Register 注册一个源类型；重复注册覆盖。
func (r *Registry) Register(s Source) {
	typ := s.Type()
	if _, exists := r.sources[typ]; !exists {
		r.order = append(r.order, typ)
	}
	r.sources[typ] = s
}

func (r *Registry) Get(typ string) (Source, bool) {
	s, ok := r.sources[typ]
	return s, ok
}

// Types 按注册顺序返回全部类型。
func (r *Registry) Types() []string {
	return append([]string(nil), r.order...)
}

// Metas 返回全部源的元数据（实现 MetaProvider 的才返回）。
func (r *Registry) Metas() []Meta {
	out := make([]Meta, 0, len(r.order))
	for _, typ := range r.order {
		if mp, ok := r.sources[typ].(MetaProvider); ok {
			out = append(out, mp.Meta())
		}
	}
	return out
}
