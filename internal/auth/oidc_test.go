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
