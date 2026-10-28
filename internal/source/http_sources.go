package source

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
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

// ---------- http_header ----------

// HTTPHeaderSource http_header 源：发送 HEAD 请求抓取响应头（如 Location 重定向）。
type HTTPHeaderSource struct{}

func (s *HTTPHeaderSource) Type() string { return "http_header" }

func (s *HTTPHeaderSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "HTTP 响应头 (重定向)",
		Icon:  "mdi-arrow-top-right-thick",
		Fields: []FieldSpec{
			{Key: "url", Label: "URL", Type: "string", Required: true, Placeholder: "https://github.com/owner/repo/releases/latest"},
			{Key: "header", Label: "响应头字段", Type: "string", Default: "Location", Help: "默认 Location（常用于捕获 30x 跳转后的真实版本）"},
			{Key: "regex", Label: "正则提取", Type: "string", Placeholder: `tag/(?:v)?([0-9.]+)`, Help: "可选，留空则取整个 Header 值"},
			{Key: "group", Label: "捕获组", Type: "string", Help: "默认 1（当配置了正则且含括号组时）"},
			{Key: "method", Label: "请求方法", Type: "select", Options: []string{"HEAD", "GET"}, Default: "HEAD"},
			{Key: "headers", Label: "自定义请求头", Type: "string", Help: "格式: Key: Value; Key2: Value2"},
		},
	}
}

func (s *HTTPHeaderSource) Validate(cfg map[string]any) error {
	if err := required(cfg, "url"); err != nil {
		return err
	}
	if re := cfgString(cfg, "regex"); re != "" {
		if _, err := regexp.Compile(re); err != nil {
			return fmt.Errorf("regex 无效: %w", err)
		}
	}
	if _, err := parseHeaders(cfgString(cfg, "headers")); err != nil {
		return err
	}
	return nil
}

func (s *HTTPHeaderSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	u := cfgString(cfg, "url")
	targetHeader := cfgString(cfg, "header")
	if targetHeader == "" {
		targetHeader = "Location"
	}
	method := strings.ToUpper(cfgString(cfg, "method"))
	if method != http.MethodGet {
		method = http.MethodHead
	}
	pattern := cfgString(cfg, "regex")
	group := atoiSafe(cfgString(cfg, "group"), 1)
	customHeaders, _ := parseHeaders(cfgString(cfg, "headers"))

	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RelayWatch/0.1 (header monitor)")
	for k, v := range customHeaders {
		req.Header.Set(k, v)
	}

	// 禁止自动重定向以捕获 Location 头
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	val := resp.Header.Get(targetHeader)
	if val == "" {
		return nil, &NotFoundError{Msg: fmt.Sprintf("未在响应中获取到头部 %s (HTTP %d)", targetHeader, resp.StatusCode)}
	}

	ver := val
	if pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("regex 无效: %w", err)
		}
		m := re.FindStringSubmatch(val)
		if m == nil {
			return nil, fmt.Errorf("响应头 %s 的值 %q 不匹配正则", targetHeader, val)
		}
		if len(m) > 1 {
			if group >= len(m) {
				return nil, fmt.Errorf("捕获组 %d 不存在（共 %d 组）", group, len(m)-1)
			}
			ver = m[group]
		} else {
			ver = m[0]
		}
	}

	return &Result{
		Version: ver,
		URL:     u,
	}, nil
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
			{Key: "image", Label: "镜像", Type: "string", Required: true, Placeholder: "nginx 或 user/repo 或 ghcr.io/owner/repo"},
			{Key: "tag_regex", Label: "Tag 正则", Type: "string", Help: "只考虑匹配的 tag，如 ^v\\d+\\.\\d+\\.\\d+$"},
			{Key: "registry", Label: "Registry", Type: "string", Help: "留空时自动从镜像名解析，默认 Docker Hub"},
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
	pattern, err := compileRegex(cfgString(cfg, "tag_regex"))
	if err != nil {
		return nil, fmt.Errorf("tag_regex 无效: %w", err)
	}

	registryHost, imagePath := splitRegistry(cfgString(cfg, "image"), cfgString(cfg, "registry"))

	// 非 Docker Hub（registry 字段或镜像名里的域名）：走标准 OCI tags API，与 container 源同一条路径
	if !isDockerHubRegistry(registryHost) {
		tags, err := fetchOCITags(ctx, registryHost, imagePath)
		if err != nil {
			return nil, err
		}
		best := pickMaxTag(tags, pattern)
		if best == "" {
			return nil, fmt.Errorf("没有匹配 tag_regex 的 tag")
		}
		return &Result{
			Version: best,
			URL:     fmt.Sprintf("https://%s/%s", registryHost, imagePath),
		}, nil
	}

	// Docker Hub：hub.docker.com v2 接口（按最近更新排序，最多扫 3 页避免限流）
	if !strings.Contains(imagePath, "/") {
		imagePath = "library/" + imagePath // 官方镜像
	}
	var best string
	pageURL := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=100", url.PathEscape(imagePath))
	for page := 0; pageURL != "" && page < 3; page++ {
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
		URL:     "https://hub.docker.com/r/" + imagePath,
	}, nil
}
