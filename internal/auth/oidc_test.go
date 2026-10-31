package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"relaywatch/internal/store"
)

func TestNormalizeIssuer(t *testing.T) {
	cases := map[string]string{
		"https://door.example/":      "https://door.example",
		"  https://door.example/  ":  "https://door.example",
		"https://door.example///":    "https://door.example",
		"https://door.example":       "https://door.example",
		"https://door.example/auth/": "https://door.example/auth",
		"/":                          "",
	}
	for in, want := range cases {
		if got := normalizeIssuer(in); got != want {
			t.Errorf("normalizeIssuer(%q) = %q, want %q", in, got, want)
		}
	}
}

// OIDC 首次登录自动建号与本地注册共用 registration.enabled 开关，默认关闭。
func TestAutoProvisionGate(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	svc := NewOIDCService(st, time.Hour, "https://rw.example")

	if svc.autoProvisionAllowed(ctx) {
		t.Error("default (no setting) should be off")
	}
	if err := st.SetSetting(ctx, "registration.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if !svc.autoProvisionAllowed(ctx) {
		t.Error("registration.enabled=true should allow auto-provision")
	}
	if err := st.SetSetting(ctx, "registration.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if svc.autoProvisionAllowed(ctx) {
		t.Error("registration.enabled=false should not allow auto-provision")
	}
}

// 绑定确认流程：回调只登记待确认项，生效必须由发起用户确认。
// 覆盖：归属校验、一次性消费、过期、身份被抢、用户已删除。
func TestBindConfirmLifecycle(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	svc := NewOIDCService(st, time.Hour, "https://rw.example")

	alice := &store.User{Username: "alice-bc", Role: "user", PasswordHash: []byte("x")}
	if err := st.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	bob := &store.User{Username: "bob-bc", Role: "user", PasswordHash: []byte("x")}
	if err := st.CreateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}

	// 模拟回调完成后的登记
	conf := svc.stageBindConfirm(alice.ID, "casdoor", &OIDCUser{Subject: "sub-1", Email: "a@idp.example"})
	if conf.Identity != "a@idp.example" || conf.Provider != "casdoor" {
		t.Fatalf("stage = %+v", conf)
	}

	// 他人查询/确认 → 拒绝
	if _, err := svc.BindConfirmInfo(bob.ID, conf.Token); err == nil {
		t.Error("BindConfirmInfo by other user should fail")
	}
	if err := svc.ConfirmBind(ctx, conf.Token, bob.ID); err == nil {
		t.Error("ConfirmBind by other user should fail")
	}

	// 归属用户查询 → OK；确认 → 落库
	info, err := svc.BindConfirmInfo(alice.ID, conf.Token)
	if err != nil || info.Identity != conf.Identity {
		t.Fatalf("BindConfirmInfo = %+v err=%v", info, err)
	}
	if err := svc.ConfirmBind(ctx, conf.Token, alice.ID); err != nil {
		t.Fatalf("ConfirmBind: %v", err)
	}
	got, err := st.UserByOIDC(ctx, "casdoor", "sub-1")
	if err != nil || got.ID != alice.ID {
		t.Fatalf("after confirm UserByOIDC = %+v err=%v", got, err)
	}

	// 一次性：重复确认 → 无效
	if err := svc.ConfirmBind(ctx, conf.Token, alice.ID); err == nil {
		t.Error("replay confirm should fail")
	}

	// 过期确认项 → 拒绝
	conf2 := svc.stageBindConfirm(alice.ID, "casdoor", &OIDCUser{Subject: "sub-2"})
	svc.mu.Lock()
	svc.bindConfirm[conf2.Token].CreatedAt = time.Now().Add(-pendingAuthTTL - time.Minute)
	svc.mu.Unlock()
	if err := svc.ConfirmBind(ctx, conf2.Token, alice.ID); err == nil {
		t.Error("expired confirm should fail")
	}

	// 身份被他人绑定 → ErrOIDCTaken
	conf3 := svc.stageBindConfirm(alice.ID, "casdoor", &OIDCUser{Subject: "sub-3"})
	if err := st.SetUserOIDC(ctx, bob.ID, "casdoor", "sub-3"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmBind(ctx, conf3.Token, alice.ID); err != ErrOIDCTaken {
		t.Errorf("confirm taken identity = %v, want ErrOIDCTaken", err)
	}

	// 发起用户被删除 → store ErrNotFound（SetUserOIDC 影响 0 行）
	if err := st.DeleteUserTransferResources(ctx, alice.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	conf4 := svc.stageBindConfirm(alice.ID, "casdoor", &OIDCUser{Subject: "sub-4"})
	if err := svc.ConfirmBind(ctx, conf4.Token, alice.ID); err == nil {
		t.Error("confirm for deleted user should fail")
	}
}
