// Package notifier 定义通知渠道接口与各渠道实现。
package notifier

import (
	"context"
	"time"
)

// MonitorInfo 消息关联的监控信息。
type MonitorInfo struct {
	Name       string
	SourceType string
	Tags       []string
	URL        string
}

// ChangeInfo 消息关联的变更信息。
type ChangeInfo struct {
	OldVersion string
	NewVersion string
	DetectedAt time.Time
}

// Message 一条待发送的通知。
type Message struct {
	Title    string
	Body     string // 已渲染的正文
	Markdown bool
	URL      string
	Monitor  MonitorInfo
	Change   ChangeInfo
}

// Notifier 推送渠道接口。
type Notifier interface {
	Type() string
	Validate(cfg map[string]any) error
	// Send 发送消息；返回 error 触发重试（Permanent 错误除外）。
	Send(ctx context.Context, cfg map[string]any, msg *Message) error
}

// FieldSpec 渠道配置字段描述（同 source.FieldSpec 结构）。
type FieldSpec = fieldSpec

type fieldSpec struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Default     any      `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
}

// Meta 渠道类型元数据。
type Meta struct {
	Type   string      `json:"type"`
	Label  string      `json:"label"`
	Icon   string      `json:"icon,omitempty"`
	Fields []FieldSpec `json:"fields"`
}

// MetaProvider 能自我描述的渠道。
type MetaProvider interface {
	Meta() Meta
}

// PermanentError 不可重试错误（400 参数错误等）。
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Registry 渠道类型注册表。
type Registry struct {
	notifiers map[string]Notifier
	order     []string
}

func NewRegistry() *Registry {
	return &Registry{notifiers: map[string]Notifier{}}
}

func (r *Registry) Register(n Notifier) {
	typ := n.Type()
	if _, ok := r.notifiers[typ]; !ok {
		r.order = append(r.order, typ)
	}
	r.notifiers[typ] = n
}

func (r *Registry) Get(typ string) (Notifier, bool) {
	n, ok := r.notifiers[typ]
	return n, ok
}

func (r *Registry) Types() []string {
	return append([]string(nil), r.order...)
}

// Metas 全部渠道元数据。
func (r *Registry) Metas() []Meta {
	out := make([]Meta, 0, len(r.order))
	for _, typ := range r.order {
		if mp, ok := r.notifiers[typ].(MetaProvider); ok {
			out = append(out, mp.Meta())
		}
	}
	return out
}

// httpPost 提供 JSON POST 能力，渠道实现共用。
var httpPost = func(ctx context.Context, url string, headers map[string]string, body []byte) (status int, respBody []byte, err error) {
	return defaultHTTPPost(ctx, url, headers, body)
}
