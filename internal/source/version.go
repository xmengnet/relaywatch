package source

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// CompareVersions 比较两个已规范化的版本号。
// 返回 -1/0/1。两边都无法解析为 semver 时退化为字符串比较，
// 一边可解析一边不可解析时也退化为字符串比较（并返回 warning）。
func CompareVersions(a, b string) (int, error) {
	if a == b {
		return 0, nil
	}
	av, aerr := semver.NewVersion(a)
	bv, berr := semver.NewVersion(b)
	if aerr != nil || berr != nil {
		// 回退：字符串比较。额外给一点数字感知：
		// "0.1.10" > "0.1.9" 这种用字符串比会错，做分段数字比较
		return compareSegmented(a, b), nil
	}
	return av.Compare(bv), nil
}

// Parseable 判断版本是否可解析为 semver。
func Parseable(v string) bool {
	_, err := semver.NewVersion(v)
	return err == nil
}

// compareSegmented 分段比较：把版本按字母/数字段切开逐段比。
// 覆盖 "0.1.9" vs "0.1.10"、"2024.01.15"、"1.0-2" 这类非严格 semver。
func compareSegmented(a, b string) int {
	as := splitVersion(a)
	bs := splitVersion(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var x, y string
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x == y {
			continue
		}
		// 两段都是纯数字：按数值比
		xn, xok := atoi(x)
		yn, yok := atoi(y)
		if xok && yok {
			switch {
			case xn < yn:
				return -1
			case xn > yn:
				return 1
			}
			continue
		}
		// 一段是数字一段是字母：数字视为更大（1.0.1 > 1.0.rc）
		if xok != yok {
			if xok {
				return 1
			}
			return -1
		}
		// 都不是数字：普通字符串比
		if x < y {
			return -1
		}
		return 1
	}
	return 0
}

var versionPartRe = regexp.MustCompile(`\d+|\D+`)

func splitVersion(v string) []string {
	parts := versionPartRe.FindAllString(strings.TrimPrefix(strings.TrimSpace(v), "v"), -1)
	return parts
}

func atoi(s string) (int64, bool) {
	var n int64
	neg := false
	i := 0
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		i = 1
	}
	if i >= len(s) {
		return 0, false
	}
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

// Normalize 应用前缀/后缀剥离，返回规范化后的版本。
func Normalize(raw, prefix, suffix string) string {
	v := strings.TrimSpace(raw)
	if prefix != "" {
		v = strings.TrimPrefix(v, prefix)
	}
	if suffix != "" {
		v = strings.TrimSuffix(v, suffix)
	}
	return v
}

// ValidateVersionFilters 校验 include/ignore 正则可编译。
func ValidateVersionFilters(include, ignore string) error {
	for name, p := range map[string]string{"version_include_regex": include, "version_ignore_regex": ignore} {
		if p == "" {
			continue
		}
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("%s 不是合法正则: %w", name, err)
		}
	}
	return nil
}
