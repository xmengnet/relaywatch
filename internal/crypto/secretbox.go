// Package crypto 提供敏感字段的 AES-256-GCM 加解密。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Box 持有主密钥（可选旧密钥用于轮换期解密）。
type Box struct {
	gcm    cipher.AEAD
	oldGcm cipher.AEAD // 可空
}

// NewBox 用 base64 编码的 32 字节密钥构造。
// oldKey 非空时启用轮换期回退解密。
func NewBox(keyB64 string, oldKeyB64 string) (*Box, error) {
	gcm, err := newGCM(keyB64)
	if err != nil {
		return nil, fmt.Errorf("secret_key: %w", err)
	}
	b := &Box{gcm: gcm}
	if oldKeyB64 != "" {
		old, err := newGCM(oldKeyB64)
		if err != nil {
			return nil, fmt.Errorf("old_secret_key: %w", err)
		}
		b.oldGcm = old
	}
	return b, nil
}

func newGCM(keyB64 string) (cipher.AEAD, error) {
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("解码失败: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("密钥需为 32 字节 base64（openssl rand -base64 32），实际 %d 字节", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

var ErrDecrypt = errors.New("解密失败")

// Encrypt 加密为 base64(nonce||ciphertext)，前缀 enc: 标记。
func (b *Box) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := b.gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "enc:" + base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt 解密 enc: 前缀的密文；无前缀原样返回（明文兼容）。
// 主密钥失败且配置了旧密钥时尝试旧密钥。
func (b *Box) Decrypt(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if !hasPrefix(s, "enc:") {
		return s, nil
	}
	raw, err := base64.StdEncoding.DecodeString(trimPrefix(s, "enc:"))
	if err != nil {
		return "", fmt.Errorf("%w: base64: %v", ErrDecrypt, err)
	}
	ns := b.gcm.NonceSize()
	if len(raw) < ns {
		return "", ErrDecrypt
	}
	plain, err := b.gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil && b.oldGcm != nil {
		plain, err = b.oldGcm.Open(nil, raw[:ns], raw[ns:], nil)
	}
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}

// Reencrypt 用主密钥重加密一段密文（轮换用）。明文输入原样返回。
func (b *Box) Reencrypt(s string) (string, error) {
	plain, err := b.Decrypt(s)
	if err != nil {
		return "", err
	}
	return b.Encrypt(plain)
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
func trimPrefix(s, p string) string {
	if hasPrefix(s, p) {
		return s[len(p):]
	}
	return s
}
