package compare

import (
	"regexp"
	"strings"
)

var (
	// epochRe 匹配 Arch Linux 风格的前导 epoch（如 1:1.0.0）
	epochRe = regexp.MustCompile(`^\d+:`)
	// pkgrelRe 匹配 Arch Linux / AUR 风格的末尾构建号（如 -1, -2）
	pkgrelRe = regexp.MustCompile(`-\d+$`)
)

// Align 按源类型对齐版本号。
// 针对 AUR 源，剥离 epoch 与 pkgrel，防止 0.1.19-1 被 semver 误判为落后于 0.1.19。
// 针对其它源，原样返回。
func Align(ver string, sourceType string) string {
	v := strings.TrimSpace(ver)
	if v == "" {
		return ""
	}
	if sourceType == "aur" {
		v = epochRe.ReplaceAllString(v, "")
		v = pkgrelRe.ReplaceAllString(v, "")
	}
	return v
}
