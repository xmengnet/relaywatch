package notify

import (
	"context"
	"errors"
	"testing"

	"relaywatch/internal/notifier"
	"relaywatch/internal/store"
)

// injectOwnerEmail：邮件渠道收件人缺省时回填渠道所有者账号邮箱。
func TestInjectOwnerEmail(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u := &store.User{Username: "alice", Role: "user", Email: "alice@example.com"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	uid := u.ID
	w := &Worker{Store: st}

	// 共享渠道（无 owner）→ 不回填
	cfg := map[string]any{"to": ""}
	w.injectOwnerEmail(ctx, &store.Channel{Type: "email_smtp"}, cfg)
	if _, ok := cfg["owner_email"]; ok {
		t.Errorf("shared channel should not get owner_email")
	}

	// 非邮件渠道 → 不回填
	cfg2 := map[string]any{"to": ""}
	w.injectOwnerEmail(ctx, &store.Channel{Type: "webhook", OwnerID: &uid}, cfg2)
	if _, ok := cfg2["owner_email"]; ok {
		t.Errorf("non-email channel should not get owner_email")
	}

	// to 已配置 → 不覆盖
	cfg3 := map[string]any{"to": "explicit@example.com"}
	w.injectOwnerEmail(ctx, &store.Channel{Type: "email_smtp", OwnerID: &uid}, cfg3)
	if _, ok := cfg3["owner_email"]; ok {
		t.Errorf("explicit to should not be overwritten")
	}

	// to 为空 + 有 owner 有邮箱 → 回填
	cfg4 := map[string]any{}
	w.injectOwnerEmail(ctx, &store.Channel{Type: "email_smtp", OwnerID: &uid}, cfg4)
	if cfg4["owner_email"] != "alice@example.com" {
		t.Errorf("owner_email = %v, want alice@example.com", cfg4["owner_email"])
	}

	// owner 无邮箱 → 不回填
	u2 := &store.User{Username: "bob", Role: "user"}
	if err := st.CreateUser(ctx, u2); err != nil {
		t.Fatal(err)
	}
	uid2 := u2.ID
	cfg5 := map[string]any{}
	w.injectOwnerEmail(ctx, &store.Channel{Type: "email_smtp", OwnerID: &uid2}, cfg5)
	if _, ok := cfg5["owner_email"]; ok {
		t.Errorf("owner without email should not get owner_email")
	}
}

// SMTPNotifier.Send：to 与 owner_email 均缺省 → 不可重试的永久错误。
func TestSMTPSendRequiresRecipient(t *testing.T) {
	n := &notifier.SMTPNotifier{}
	err := n.Send(context.Background(), map[string]any{
		"host": "smtp.example.com", "port": "587", "username": "u", "password": "p", "from": "rw@example.com",
	}, &notifier.Message{Title: "t", Body: "b"})
	var pe *notifier.PermanentError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want PermanentError", err)
	}
}
