package notify

import (
	"context"
	"encoding/json"
	"fmt"

	"relaywatch/internal/notifier"
	"relaywatch/internal/store"
)

// TestSenderFunc 渠道测试发送：绕过投递队列，用默认模板直接发一条。
type TestSenderFunc func(ctx context.Context, ch *store.Channel) error

// NewTestSender 构造渠道测试发送函数，供 HTTP 层注入。
func NewTestSender(reg *notifier.Registry, st store.Store) TestSenderFunc {
	return func(ctx context.Context, ch *store.Channel) error {
		n, ok := reg.Get(ch.Type)
		if !ok {
			return fmt.Errorf("未知渠道类型 %s", ch.Type)
		}
		var cfgMap map[string]any
		_ = json.Unmarshal([]byte(ch.Config), &cfgMap)
		if cfgMap == nil {
			cfgMap = map[string]any{}
		}
		// 与投递路径一致：邮件渠道收件人缺省时回填渠道所有者邮箱
		if ch.Type == "email_smtp" {
			if to, _ := cfgMap["to"].(string); to == "" && ch.OwnerID != nil && *ch.OwnerID != 0 {
				if u, err := st.UserByID(ctx, *ch.OwnerID); err == nil && u.Email != "" {
					cfgMap["owner_email"] = u.Email
				}
			}
		}
		msg := &notifier.Message{
			Title:    "RelayWatch 测试消息",
			Body:     "这是一条来自 RelayWatch 的测试通知，说明该渠道配置正确。",
			Markdown: true,
		}
		return n.Send(ctx, cfgMap, msg)
	}
}
