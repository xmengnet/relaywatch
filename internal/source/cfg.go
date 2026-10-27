package source

import (
	"fmt"
	"regexp"
	"strings"
)

// cfgString 取配置字符串字段。
func cfgString(cfg map[string]any, key string) string {
	v, _ := cfg[key].(string)
	return strings.TrimSpace(v)
}

// cfgBool 取配置布尔字段（接受 bool 或字符串 "true"）。
func cfgBool(cfg map[string]any, key string) bool {
	switch v := cfg[key].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1" || v == "yes"
	}
	return false
}

// required 校验必填字符串字段。
func required(cfg map[string]any, keys ...string) error {
	for _, k := range keys {
		if cfgString(cfg, k) == "" {
			return fmt.Errorf("缺少必填参数: %s", k)
		}
	}
	return nil
}

// compileRegex 编译用户提供的正则；空串返回 nil。
func compileRegex(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}
