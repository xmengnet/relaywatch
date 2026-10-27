// Package importer 把 nvchecker keyfile（TOML）转换为 RelayWatch 监控记录。
package importer

import (
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Item 一条转换结果。
type Item struct {
	Name         string
	SourceType   string
	SourceConfig map[string]any
	VersionPrefix string
	IncludeRegex  string
	IgnoreRegex   string
}

// Result 解析结果：可导入项 + 告警（未识别字段/类型）。
type Result struct {
	Items    []Item
	Warnings []string // 与条目无关的全局告警
	Ignored  []string // 被忽略的条目及原因
}

// fieldMap 把 nvchecker 源选项映射到 RelayWatch 源参数。
// 返回 (sourceType, config, ok)
func fieldMap(name string, opts map[string]any) (string, map[string]any, bool) {
	str := func(k string) string { s, _ := opts[k].(string); return s }
	boolish := func(k string) bool {
		switch v := opts[k].(type) {
		case bool:
			return v
		case string:
			return v == "true" || v == "1"
		}
		return false
	}
	switch str("source") {
	case "github":
		repo := str("github")
		if repo == "" {
			return "", nil, false
		}
		cfg := map[string]any{"repo": repo}
		if b := boolish("use_latest_release"); b {
			return "github_release", cfg, true
		}
		return "github_tag", cfg, true
	case "gitlab":
		host := str("gitlab")
		project := str("project")
		if host == "" || project == "" {
			return "", nil, false
		}
		return "gitlab_release", map[string]any{"host": host, "project": project}, true
	case "gitea":
		host, repo := str("gitea"), str("gitea_repo")
		if host == "" || repo == "" {
			return "", nil, false
		}
		return "gitea_release", map[string]any{"host": host, "repo": repo}, true
	case "aur":
		pkg := str("aur")
		if pkg == "" {
			return "", nil, false
		}
		return "aur", map[string]any{"pkgname": pkg}, true
	case "archpkg":
		pkg := str("archpkg")
		if pkg == "" {
			return "", nil, false
		}
		cfg := map[string]any{"pkgname": pkg}
		if r := str("strip_release"); r != "" {
			// nvchecker 无对应概念，忽略
			_ = r
		}
		return "arch_official", cfg, true
	case "pypi":
		pkg := str("pypi")
		if pkg == "" {
			return "", nil, false
		}
		return "pypi", map[string]any{"project": pkg}, true
	case "npm":
		pkg := str("npm")
		if pkg == "" {
			return "", nil, false
		}
		return "npm", map[string]any{"package": pkg}, true
	case "cratesio":
		pkg := str("cratesio")
		if pkg == "" {
			return "", nil, false
		}
		return "crates", map[string]any{"crate": pkg}, true
	case "goproxy", "":
		// nvchecker 的 go module 检查用 module path 做 key 或 source=goproxy
		mod := str("goproxy")
		if mod == "" {
			// 无 source 字段且只有 module 风格键 → 尝试 go_module
			if m := str("module"); m != "" {
				return "go_module", map[string]any{"module": m}, true
			}
		}
		if mod != "" {
			return "go_module", map[string]any{"module": mod}, true
		}
	case "httpheader", "regex", "htmlparser":
		u := str("url")
		if u == "" {
			return "", nil, false
		}
		cfg := map[string]any{"url": u}
		if re := str("regex"); re != "" {
			cfg["regex"] = re
			return "http_regex", cfg, true
		}
		if jp := str("jsonpath"); jp != "" {
			cfg["path"] = jp
			return "http_json", cfg, true
		}
		return "http_regex", cfg, true
	case "container":
		img := str("container")
		if img == "" {
			return "", nil, false
		}
		return "docker_tag", map[string]any{"image": img}, true
	}
	return "", nil, false
}

// knownGlobal 是 nvchecker 全局/每条可选字段中我们理解或安全忽略的。
var knownGlobal = map[string]bool{
	"source": true, "prefix": true, "suffix": true,
	"include_regex": true, "exclude_regex": true, "from_pattern": true, "to_pattern": true,
	"use_latest_release": true, "use_max_tag": true, "use_sorted_tags": true,
	"github": true, "gitlab": true, "project": true, "gitea": true, "gitea_repo": true,
	"aur": true, "archpkg": true, "pypi": true, "npm": true, "cratesio": true,
	"goproxy": true, "module": true, "url": true, "regex": true, "jsonpath": true,
	"container": true, "token": true, "token_env": true,
}

// Parse 解析 nvchecker keyfile TOML 文本。
func Parse(text string) (*Result, error) {
	var doc map[string]map[string]any
	if err := toml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, fmt.Errorf("TOML 解析失败: %w", err)
	}
	res := &Result{}
	// __config__ 全局段
	if cfg, ok := doc["__config__"]; ok {
		_ = cfg
		res.Warnings = append(res.Warnings, "已忽略 __config__ 全局段（全网代理等设置不适用）")
	}
	for name, opts := range doc {
		if name == "__config__" {
			continue
		}
		if strings.HasPrefix(name, "__") {
			continue
		}
		sourceType, cfg, ok := fieldMap(name, opts)
		if !ok {
			// 找出第一个不认识的键提示
			unknown := ""
			for k := range opts {
				if !knownGlobal[k] {
					unknown = k
					break
				}
			}
			res.Ignored = append(res.Ignored, fmt.Sprintf("%s: 不支持的源类型%s", name, noteUnknown(unknown)))
			continue
		}
		item := Item{Name: name, SourceType: sourceType, SourceConfig: cfg}
		if p, _ := opts["prefix"].(string); p != "" {
			item.VersionPrefix = p
		}
		if ir, _ := opts["include_regex"].(string); ir != "" {
			item.IncludeRegex = ir
		}
		if er, _ := opts["exclude_regex"].(string); er != "" {
			item.IgnoreRegex = er
		}
		// 警告我们不支持的转换
		if _, has := opts["from_pattern"]; has {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: from_pattern/to_pattern 版本重写暂不支持，按原值比较", name))
		}
		res.Items = append(res.Items, item)
	}
	return res, nil
}

func noteUnknown(k string) string {
	if k == "" {
		return ""
	}
	return fmt.Sprintf("（无法识别字段 %q）", k)
}
