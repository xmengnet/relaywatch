package source

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// HTTPJSONSource http_json 源：任意 JSON 接口 + gjson 风格点路径提取。
// 实现内嵌轻量路径取值（a.b.0.c），不引完整 gjson。
type HTTPJSONSource struct{}

func (s *HTTPJSONSource) Type() string { return "http_json" }

func (s *HTTPJSONSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "HTTP JSON",
		Icon:  "mdi-code-json",
		Fields: []FieldSpec{
			{Key: "url", Label: "URL", Type: "string", Required: true, Placeholder: "https://example.com/api/version"},
			{Key: "path", Label: "版本字段路径", Type: "string", Required: true, Placeholder: "data.version", Help: "点分隔，数组用数字下标"},
			{Key: "headers", Label: "请求头", Type: "string", Help: "格式: Key: Value; Key2: Value2"},
		},
	}
}

func (s *HTTPJSONSource) Validate(cfg map[string]any) error {
	if err := required(cfg, "url", "path"); err != nil {
		return err
	}
	if _, err := parseHeaders(cfgString(cfg, "headers")); err != nil {
		return err
	}
	return nil
}

func (s *HTTPJSONSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	u := cfgString(cfg, "url")
	path := cfgString(cfg, "path")
	headers, _ := parseHeaders(cfgString(cfg, "headers"))

	var doc any
	if err := fetchJSON(ctx, Options{}, u, headers, &doc); err != nil {
		return nil, err
	}
	val, ok := jsonLookup(doc, path)
	if !ok || val == "" {
		return nil, fmt.Errorf("路径 %q 未取到值", path)
	}
	return &Result{Version: val, URL: u}, nil
}

// jsonLookup 点路径取值：键名或数字下标。
func jsonLookup(doc any, path string) (string, bool) {
	cur := doc
	if path == "" {
		return stringify(doc)
	}
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return "", false
			}
			cur = v
		case []any:
			idx := -1
			for _, c := range seg {
				if c < '0' || c > '9' {
					idx = -1
					break
				}
				if idx < 0 {
					idx = 0
				}
				idx = idx*10 + int(c-'0')
			}
			if idx < 0 || idx >= len(node) {
				return "", false
			}
			cur = node[idx]
		default:
			return "", false
		}
	}
	return stringify(cur)
}

func stringify(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case float64:
		return fmt.Sprintf("%v", t), true
	default:
		return "", false
	}
}

// parseHeaders 解析 "Key: Value; K2: V2" 形式的请求头。
func parseHeaders(s string) (map[string]string, error) {
	out := map[string]string{}
	if s == "" {
		return out, nil
	}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("请求头格式错误（应 Key: Value）: %q", part)
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, nil
}

// ---------- http_regex ----------

// HTTPRegexSource http_regex 源：抓网页用正则提取版本。
type HTTPRegexSource struct{}

func (s *HTTPRegexSource) Type() string { return "http_regex" }

func (s *HTTPRegexSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "HTTP 正则抓取",
		Icon:  "mdi-regex",
		Fields: []FieldSpec{
			{Key: "url", Label: "URL", Type: "string", Required: true},
			{Key: "regex", Label: "正则", Type: "string", Required: true, Placeholder: `version["']?\s*[:=]\s*["']([\d.]+)`},
			{Key: "group", Label: "捕获组", Type: "string", Help: "默认 1（第一个捕获组）"},
			{Key: "headers", Label: "请求头", Type: "string"},
		},
	}
}

func (s *HTTPRegexSource) Validate(cfg map[string]any) error {
	if err := required(cfg, "url", "regex"); err != nil {
		return err
	}
	if _, err := regexp.Compile(cfgString(cfg, "regex")); err != nil {
		return fmt.Errorf("regex 无效: %w", err)
	}
	if _, err := parseHeaders(cfgString(cfg, "headers")); err != nil {
		return err
	}
	return nil
}

func (s *HTTPRegexSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	u := cfgString(cfg, "url")
	pattern := cfgString(cfg, "regex")
	group := atoiSafe(cfgString(cfg, "group"), 1)
	headers, _ := parseHeaders(cfgString(cfg, "headers"))

	body, err := fetchText(ctx, Options{}, u, headers)
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("regex 无效: %w", err)
	}
	m := re.FindStringSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("页面内容不匹配正则")
	}
	if group >= len(m) {
		return nil, fmt.Errorf("捕获组 %d 不存在（共 %d 组）", group, len(m)-1)
	}
	if m[group] == "" {
		return nil, fmt.Errorf("捕获组 %d 为空", group)
	}
	return &Result{Version: m[group], URL: u}, nil
}

func atoiSafe(s string, def int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return def
	}
	return n
}

// ---------- docker_tag ----------

// DockerTagSource docker_tag 源：Docker Hub / registry 的最大 tag。
type DockerTagSource struct{}

func (s *DockerTagSource) Type() string { return "docker_tag" }

func (s *DockerTagSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "容器镜像",
		Icon:  "mdi-docker",
		Fields: []FieldSpec{
			{Key: "image", Label: "镜像", Type: "string", Required: true, Placeholder: "library/nginx 或 user/repo"},
			{Key: "tag_regex", Label: "Tag 正则", Type: "string", Help: "只考虑匹配的 tag，如 ^v\\d+\\.\\d+\\.\\d+$"},
			{Key: "registry", Label: "Registry", Type: "string", Help: "默认 Docker Hub"},
		},
	}
}

func (s *DockerTagSource) Validate(cfg map[string]any) error {
	if err := required(cfg, "image"); err != nil {
		return err
	}
	return ValidateVersionFilters("", cfgString(cfg, "tag_regex"))
}

type dockerTagsResp struct {
	Results []struct {
		Name string `json:"name"`
	} `json:"results"`
	Next string `json:"next"`
}

func (s *DockerTagSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	image := strings.Trim(cfgString(cfg, "image"), "/")
	if !strings.Contains(image, "/") {
		image = "library/" + image // Docker Hub 官方镜像
	}
	pattern, err := compileRegex(cfgString(cfg, "tag_regex"))
	if err != nil {
		return nil, fmt.Errorf("tag_regex 无效: %w", err)
	}

	var best string
	pageURL := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=100", url.PathEscape(image))
	for page := 0; pageURL != "" && page < 3; page++ {
		// 最多扫 3 页（300 个 tag），避免被限流
		var resp dockerTagsResp
		if err := fetchJSON(ctx, Options{}, pageURL, nil, &resp); err != nil {
			return nil, err
		}
		for _, r := range resp.Results {
			if pattern != nil && !pattern.MatchString(r.Name) {
				continue
			}
			if best == "" || greaterVersion(r.Name, best) {
				best = r.Name
			}
		}
		pageURL = resp.Next
	}
	if best == "" {
		return nil, fmt.Errorf("没有匹配的 tag")
	}
	return &Result{
		Version: best,
		URL:     "https://hub.docker.com/r/" + image,
	}, nil
}
