package store

import (
	"testing"
	"time"
)

// 绑定/解绑：UNIQUE (oidc_provider, oidc_subject) 兜底身份唯一性；
// 解绑必须置 NULL（空串会互相违反 UNIQUE）。
func TestOIDCBindUnbind(t *testing.T) {
	st, ctx := newMultiUserStore(t)
	alice := &User{Username: "alice", Role: "user", CreatedAt: time.Now()}
	if err := st.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	bob := &User{Username: "bob", Role: "user", CreatedAt: time.Now()}
	if err := st.CreateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}

	if err := st.SetUserOIDC(ctx, alice.ID, "casdoor", "sub-1"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	got, err := st.UserByOIDC(ctx, "casdoor", "sub-1")
	if err != nil || got.ID != alice.ID {
		t.Fatalf("UserByOIDC = (%v, %v), want alice", got, err)
	}

	// 同一身份绑给第二个用户 → UNIQUE 约束拒绝
	if err := st.SetUserOIDC(ctx, bob.ID, "casdoor", "sub-1"); err == nil {
		t.Fatal("binding same identity to a second user should violate UNIQUE")
	}

	// 解绑置 NULL：查不到、可重复解绑、解绑后可重新绑定
	if err := st.UnbindOIDCUser(ctx, alice.ID); err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if _, err := st.UserByOIDC(ctx, "casdoor", "sub-1"); err != ErrNotFound {
		t.Fatalf("after unbind err = %v, want ErrNotFound", err)
	}
	if err := st.UnbindOIDCUser(ctx, alice.ID); err != nil {
		t.Fatalf("repeat unbind: %v", err)
	}
	if err := st.SetUserOIDC(ctx, alice.ID, "casdoor", "sub-1"); err != nil {
		t.Fatalf("re-bind after unbind: %v", err)
	}
}
