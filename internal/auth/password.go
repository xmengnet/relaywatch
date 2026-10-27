package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword 生成 bcrypt 哈希，cost 12。
func HashPassword(pw string) ([]byte, error) {
	if len(pw) < 8 {
		return nil, fmt.Errorf("password too short: min 8 chars")
	}
	return bcrypt.GenerateFromPassword([]byte(pw), 12)
}

// ComparePassword 校验密码。
func ComparePassword(hash []byte, pw string) bool {
	return bcrypt.CompareHashAndPassword(hash, []byte(pw)) == nil
}

// RandomToken 生成 n 字节密码学随机的 base64url 字符串。
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
