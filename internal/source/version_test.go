package source

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.19", "0.1.19", 0},
		{"0.1.19", "0.1.20", -1},
		{"0.1.9", "0.1.10", -1},       // 字符串比会错，分段比正确
		{"1.2.3", "1.2.3-rc1", 1},     // 正式 > 预发布
		{"1.2.3-rc1", "1.2.3-rc2", -1},
		{"2024.01.15", "2024.02.01", -1},
		{"1.0-2", "1.0-3", -1},        // pkgrel 风格
		{"0.5-2", "0.4.9", 1},
		{"2.0", "10.0", -1},
		{"v1.0", "v1.0", 0},           // 前缀剥离前字符串比较——由调用方规范化
	}
	for _, c := range cases {
		got, err := CompareVersions(c.a, c.b)
		if err != nil {
			t.Fatalf("CompareVersions(%q,%q) err: %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("CompareVersions(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct {
		raw, prefix, suffix, want string
	}{
		{"v0.1.19", "v", "", "0.1.19"},
		{"0.1.19-1", "", "-1", "0.1.19"},
		{"  1.2.3  ", "", "", "1.2.3"},
		{"v1.0", "", "", "v1.0"}, // 无前缀配置时保留
	}
	for _, c := range cases {
		got := Normalize(c.raw, c.prefix, c.suffix)
		if got != c.want {
			t.Errorf("Normalize(%q,%q,%q) = %q, want %q", c.raw, c.prefix, c.suffix, got, c.want)
		}
	}
}

func TestRewriteVersion(t *testing.T) {
	cases := []struct {
		raw, from, to, want string
	}{
		{"20261027", `(\d{4})(\d{2})(\d{2})`, `\1.\2.\3`, "2026.10.27"},
		{"v1.2.3-release", `release`, `final`, "v1.2.3-final"},
		{"abc-123", `abc-(\d+)`, `${1}`, "123"},
		{"build_987", `build_(?P<num>\d+)`, `\g<num>`, "987"},
		{"no-match", `^foo`, `bar`, "no-match"},
		{"1.0.0", "", "something", "1.0.0"},
	}
	for _, c := range cases {
		got, err := RewriteVersion(c.raw, c.from, c.to)
		if err != nil {
			t.Fatalf("RewriteVersion(%q, %q, %q) err: %v", c.raw, c.from, c.to, err)
		}
		if got != c.want {
			t.Errorf("RewriteVersion(%q, %q, %q) = %q, want %q", c.raw, c.from, c.to, got, c.want)
		}
	}
}

