package source

import (
	"errors"
	"net/url"
	"strings"
	"sync"
)

// errInvalidMirror 镜像地址非法。
var errInvalidMirror = errors.New("镜像地址必须是合法的绝对 URL")

// 站点级 URL 配置（「网站管理」页维护，settings 表持久化）：
//   - Mirrors:  上游 API 域名 → 镜像/代理前缀（如 GitHub 走加速代理）
//   - HostDefaults: 源类型 → 自建服务站点默认值（如 gitea_release → 内网 Gitea 地址）

// MirrorRule 一条 API 镜像规则：hostPrefix 命中则整段 URL 前缀替换为 replaceWith。
type MirrorRule struct {
	From string `json:"from"` // 原始前缀，如 https://api.github.com
	To   string `json:"to"`   // 镜像前缀，如 https://gh-proxy.example/api.github.com
}

// HostDefault 源类型的站点默认值：该源监控的 host 字段留空时回填。
type HostDefault struct {
	SourceType string `json:"source_type"`
	Host       string `json:"host"`
}

var (
	siteMu   sync.RWMutex
	mirrors  []MirrorRule
	hostDefs []HostDefault
)

// SetSiteConfig 注入站点级 URL 配置（Checker 初始化时调用，运行时更新原子替换）。
func SetSiteConfig(ms []MirrorRule, defs []HostDefault) {
	siteMu.Lock()
	defer siteMu.Unlock()
	mirrors = ms
	hostDefs = defs
}

// RewriteURL 按 mirrors 规则改写上游 API 地址；无命中原样返回。
func RewriteURL(raw string) string {
	siteMu.RLock()
	defer siteMu.RUnlock()
	for _, m := range mirrors {
		if m.From == "" || m.To == "" {
			continue
		}
		if strings.HasPrefix(raw, m.From) {
			return m.To + strings.TrimPrefix(raw, m.From)
		}
	}
	return raw
}

// HostDefaultFor 查某源类型的站点默认值；无则空串。
func HostDefaultFor(sourceType string) string {
	siteMu.RLock()
	defer siteMu.RUnlock()
	for _, d := range hostDefs {
		if d.SourceType == sourceType {
			return strings.TrimRight(d.Host, "/")
		}
	}
	return ""
}

// ApplyHostDefaults 把站点默认值回填进源配置：host 字段缺失或为空时填充。
// 无默认值或已配置时原样返回（cfg 就地修改并返回）。
func ApplyHostDefaults(sourceType string, cfg map[string]any) map[string]any {
	def := HostDefaultFor(sourceType)
	if def == "" || cfg == nil {
		return cfg
	}
	if v, ok := cfg["host"].(string); !ok || strings.TrimSpace(v) == "" {
		cfg["host"] = def
	}
	return cfg
}

// RewriteURLInput 解析并校验镜像规则；保证 from 是合法绝对 URL 前缀、to 无尾斜杠。
func RewriteURLInput(from, to string) (string, string, error) {
	from = strings.TrimRight(strings.TrimSpace(from), "/")
	to = strings.TrimRight(strings.TrimSpace(to), "/")
	if from == "" || to == "" {
		return "", "", errInvalidMirror
	}
	u, err := url.Parse(from)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", errInvalidMirror
	}
	u2, err := url.Parse(to)
	if err != nil || u2.Scheme == "" || u2.Host == "" {
		return "", "", errInvalidMirror
	}
	return from, to, nil
}
