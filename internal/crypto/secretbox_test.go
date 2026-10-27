package crypto

import (
	"strings"
	"testing"
)

func newTestBox(t *testing.T) *Box {
	t.Helper()
	// 32 字节 "0123456789abcdef0123456789abcdef" 的 base64
	key := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	b, err := NewBox(key, "")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	b := newTestBox(t)
	plain := `{"token":"ghp_secret123","url":"https://x"}`
	enc, err := b.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "enc:") {
		t.Errorf("ciphertext missing prefix: %q", enc[:10])
	}
	if strings.Contains(enc, "ghp_secret") {
		t.Error("ciphertext leaks plaintext")
	}
	got, err := b.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Errorf("roundtrip = %q, want %q", got, plain)
	}
}

func TestDecryptPlainPassthrough(t *testing.T) {
	b := newTestBox(t)
	got, err := b.Decrypt("plain-old-value")
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain-old-value" {
		t.Errorf("plain passthrough = %q", got)
	}
	if _, err := b.Decrypt(""); err != nil {
		t.Error("empty should be no-op")
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	b1 := newTestBox(t)
	enc, _ := b1.Encrypt("hello")

	key2 := "OTg3NjU0MzIxMGFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	b2, err := NewBox(key2, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b2.Decrypt(enc); err == nil {
		t.Error("expected decrypt failure with wrong key")
	}
}

func TestOldKeyFallback(t *testing.T) {
	oldKey := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	newKey := "OTg3NjU0MzIxMGFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	oldBox, _ := NewBox(oldKey, "")
	enc, _ := oldBox.Encrypt("legacy-secret")

	// 新密钥 + 旧密钥回退
	rotated, err := NewBox(newKey, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	got, err := rotated.Decrypt(enc)
	if err != nil {
		t.Fatalf("old key fallback failed: %v", err)
	}
	if got != "legacy-secret" {
		t.Errorf("got %q", got)
	}

	// Reencrypt 后用新密钥可解
	re, err := rotated.Reencrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	bare, _ := NewBox(newKey, "")
	if got, err := bare.Decrypt(re); err != nil || got != "legacy-secret" {
		t.Errorf("reencrypt: %q %v", got, err)
	}
}

func TestInvalidKey(t *testing.T) {
	if _, err := NewBox("short", ""); err == nil {
		t.Error("expected error for short key")
	}
	if _, err := NewBox("!!!not-base64!!!", ""); err == nil {
		t.Error("expected error for bad base64")
	}
}
