// Package importer 把 nvchecker keyfile（TOML）转换为 RelayWatch 监控记录。
package importer

import (
	"fmt"
	"strings"

	"relaywatch/internal/source"

	"github.com/pelletier/go-toml/v2"
)

// Item 一条转换结果。
type Item struct {
	Name          string
	SourceType    string
	SourceConfig  map[string]any
	VersionPrefix string
	IncludeRegex  string
	IgnoreRegex   string
	FromPattern   string
	ToPattern     string
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
	case "httpheader":
		u := str("url")
		if u == "" {
			return "", nil, false
		}
		cfg := map[string]any{"url": u}
		if h := str("header"); h != "" {
			cfg["header"] = h
		}
		if re := str("regex"); re != "" {
			cfg["regex"] = re
		}
		return "http_header", cfg, true
	case "regex", "htmlparser":
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
	case "git", "git_dulwich", "git_pygit2":
		gitURL := str("git")
		if gitURL == "" {
			return "", nil, false
		}
		cfg := map[string]any{"url": gitURL}
		if br := str("branch"); br != "" {
			cfg["branch"] = br
		}
		if boolish("use_commit") {
			cfg["use_commit"] = true
		}
		return "git_tag", cfg, true
	case "bitbucket":
		repo := str("bitbucket")
		if repo == "" {
			return "", nil, false
		}
		cfg := map[string]any{"repo": repo}
		if br := str("branch"); br != "" {
			cfg["branch"] = br
		}
		return "bitbucket", cfg, true
	case "pagure":
		repo := str("pagure")
		if repo == "" {
			return "", nil, false
		}
		return "pagure", map[string]any{"repo": repo}, true
	case "container":
		img := str("container")
		if img == "" {
			return "", nil, false
		}
		cfg := map[string]any{"image": img}
		if reg := str("registry"); reg != "" {
			cfg["registry"] = reg
		}
		return "container", cfg, true
	case "debianpkg":
		pkg := str("debianpkg")
		if pkg == "" {
			pkg = name
		}
		cfg := map[string]any{"pkgname": pkg}
		if s := str("suite"); s != "" {
			cfg["suite"] = s
		}
		if boolish("strip_release") {
			cfg["strip_release"] = true
		}
		return "debianpkg", cfg, true
	case "ubuntupkg":
		pkg := str("ubuntupkg")
		if pkg == "" {
			pkg = name
		}
		cfg := map[string]any{"pkgname": pkg}
		if s := str("suite"); s != "" {
			cfg["suite"] = s
		}
		if boolish("strip_release") {
			cfg["strip_release"] = true
		}
		return "ubuntupkg", cfg, true
	case "cpan":
		pkg := str("cpan")
		if pkg == "" {
			pkg = name
		}
		return "cpan", map[string]any{"module": pkg}, true
	case "cran":
		pkg := str("cran")
		if pkg == "" {
			pkg = name
		}
		return "cran", map[string]any{"package": pkg}, true
	case "hackage":
		pkg := str("hackage")
		if pkg == "" {
			pkg = name
		}
		return "hackage", map[string]any{"package": pkg}, true
	case "repology":
		project := str("repology")
		if project == "" {
			project = name
		}
		repo := str("repo")
		if repo == "" {
			return "", nil, false
		}
		cfg := map[string]any{"project": project, "repo": repo}
		if sub := str("subrepo"); sub != "" {
			cfg["subrepo"] = sub
		}
		return "repology", cfg, true
	case "anitya":
		pkg := str("anitya_id")
		if pkg == "" {
			pkg = str("anitya")
		}
		if pkg == "" {
			pkg = name
		}
		return "anitya", map[string]any{"project": pkg}, true
	case "gems", "rubygems":
		gem := str("gems")
		if gem == "" {
			gem = str("rubygems")
		}
		if gem == "" {
			gem = name
		}
		cfg := map[string]any{"gem": gem}
		if boolish("include_prereleases") || boolish("include_prerelease") {
			cfg["include_prerelease"] = true
		}
		return "rubygems", cfg, true
	case "maven":
		group := str("group")
		artifact := str("artifact")
		if group == "" || artifact == "" {
			return "", nil, false
		}
		cfg := map[string]any{"group": group, "artifact": artifact}
		if repo := str("repo"); repo != "" {
			cfg["repo"] = repo
		}
		return "maven", cfg, true
	case "packagist":
		pkg := str("packagist")
		if pkg == "" {
			pkg = name
		}
		cfg := map[string]any{"package": pkg}
		if boolish("include_prereleases") || boolish("include_prerelease") {
			cfg["include_prerelease"] = true
		}
		return "packagist", cfg, true
	case "pacman":
		pkg := str("pacman")
		if pkg == "" {
			pkg = name
		}
		return "arch_official", map[string]any{"pkgname": pkg}, true
	case "alpm":
		pkg := str("alpm")
		if pkg == "" {
			pkg = name
		}
		cfg := map[string]any{"pkgname": pkg}
		if repo := str("repo"); repo != "" {
			cfg["repo"] = repo
		}
		return "arch_official", cfg, true
	case "openvsx":
		ext := str("openvsx")
		if ext == "" {
			ext = name
		}
		return "openvsx", map[string]any{"extension": ext}, true
	case "vsmarketplace":
		ext := str("vsmarketplace")
		if ext == "" {
			ext = name
		}
		return "vsmarketplace", map[string]any{"extension": ext}, true
	case "snapcraft":
		snap := str("snapcraft")
		if snap == "" {
			snap = str("snap")
		}
		if snap == "" {
			snap = name
		}
		cfg := map[string]any{"snap": snap}
		if ch := str("channel"); ch != "" {
			cfg["channel"] = ch
		}
		return "snapcraft", cfg, true
	case "manual":
		ver := str("manual")
		if ver == "" {
			return "", nil, false
		}
		cfg := map[string]any{"version": ver}
		if u := str("url"); u != "" {
			cfg["url"] = u
		}
		return "manual", cfg, true
	case "cmd":
		cmdStr := str("cmd")
		if cmdStr == "" {
			return "", nil, false
		}
		if !source.EnableCmdSource {
			return "", nil, false
		}
		return "cmd", map[string]any{"cmd": cmdStr}, true
	}
	return "", nil, false
}

// knownGlobal 是 nvchecker 全局/每条可选字段中我们理解或安全忽略的。
var knownGlobal = map[string]bool{
	"source": true, "prefix": true, "suffix": true,
	"include_regex": true, "exclude_regex": true, "from_pattern": true, "to_pattern": true,
	"use_latest_release": true, "use_max_tag": true, "use_sorted_tags": true,
	"include_prereleases": true, "include_prerelease": true, "ignored": true,
	"github": true, "gitlab": true, "project": true, "gitea": true, "gitea_repo": true,
	"aur": true, "archpkg": true, "pacman": true, "alpm": true, "pypi": true, "npm": true, "cratesio": true,
	"goproxy": true, "module": true, "url": true, "regex": true, "jsonpath": true,
	"container": true, "token": true, "token_env": true,
	"repology": true, "repo": true, "subrepo": true, "anitya": true, "anitya_id": true,
	"gems": true, "rubygems": true, "maven": true, "group": true, "artifact": true,
	"packagist": true, "strip_release": true, "dbpath": true,
	"git": true, "bitbucket": true, "pagure": true, "branch": true, "use_commit": true,
	"header": true, "method": true,
	"debianpkg": true, "ubuntupkg": true, "cpan": true, "cran": true, "hackage": true,
	"registry": true, "suite": true,
	"openvsx": true, "vsmarketplace": true, "snapcraft": true, "snap": true, "channel": true,
	"manual": true, "cmd": true, "timeout": true,
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
			src, _ := opts["source"].(string)
			if src == "cmd" && !source.EnableCmdSource {
				res.Ignored = append(res.Ignored, fmt.Sprintf("%s: 出于安全考虑已跳过本地 shell 命令条目（如需启用请在服务端配置 RELAYWATCH_ENABLE_CMD_SOURCE=1）", name))
				continue
			}
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
		if fp, _ := opts["from_pattern"].(string); fp != "" {
			item.FromPattern = fp
		}
		if tp, _ := opts["to_pattern"].(string); tp != "" {
			item.ToPattern = tp
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
