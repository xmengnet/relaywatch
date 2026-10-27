package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ContainerSource container 源：遵循标准 OCI Registry V2 规范查询镜像 tags。
// 原生支持 Docker Hub, GHCR (ghcr.io), Quay (quay.io), registry.k8s.io 及各类私有 Harbor 仓库。
type ContainerSource struct{}

func (s *ContainerSource) Type() string { return "container" }

func (s *ContainerSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "容器镜像 (OCI Registry)",
		Icon:  "mdi-docker",
		Fields: []FieldSpec{
			{Key: "image", Label: "镜像名称", Type: "string", Required: true, Placeholder: "nginx 或 library/nginx 或 ghcr.io/owner/repo", Help: "可直接包含仓库域名"},
			{Key: "registry", Label: "Registry 域名", Type: "string", Placeholder: "registry-1.docker.io", Help: "留空时自动从镜像名称解析，默认 Docker Hub"},
			{Key: "tag_regex", Label: "Tag 正则", Type: "string", Help: "仅考虑匹配的 tag，如 ^v[0-9.]+$"},
		},
	}
}

func (s *ContainerSource) Validate(cfg map[string]any) error {
	if err := required(cfg, "image"); err != nil {
		return err
	}
	return ValidateVersionFilters("", cfgString(cfg, "tag_regex"))
}

// oidcAuthInfo 记录 OCI 401 响应解析后的认证端点
type ociAuthInfo struct {
	Realm   string
	Service string
}

func parseWWWAuthenticate(header string) (*ociAuthInfo, error) {
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return nil, fmt.Errorf("不支持的认证类型: %s", header)
	}
	raw := strings.TrimPrefix(header[7:], " ")
	info := &ociAuthInfo{}
	// 解析形如 realm="https://...",service="...",scope="..."
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.Trim(strings.TrimSpace(v), `"`)
		switch k {
		case "realm":
			info.Realm = v
		case "service":
			info.Service = v
		}
	}
	if info.Realm == "" {
		return nil, fmt.Errorf("WWW-Authenticate 中缺少 realm")
	}
	return info, nil
}

func fetchOCITags(ctx context.Context, registryHost, imagePath string) ([]string, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	scheme := "https"
	if strings.HasPrefix(registryHost, "127.0.0.1") || strings.HasPrefix(registryHost, "localhost") {
		scheme = "http"
	}

	// 1. 发送探针请求获取认证信息
	probeURL := fmt.Sprintf("%s://%s/v2/", scheme, registryHost)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RelayWatch/0.1 (OCI Client)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("探测 OCI registry 失败: %w", err)
	}
	defer resp.Body.Close()

	var token string
	if resp.StatusCode == http.StatusUnauthorized {
		authHdr := resp.Header.Get("Www-Authenticate")
		if authHdr == "" {
			authHdr = resp.Header.Get("WWW-Authenticate")
		}
		authInfo, err := parseWWWAuthenticate(authHdr)
		if err != nil {
			return nil, fmt.Errorf("解析 OCI 鉴权头失败: %w", err)
		}

		// 2. 换取 Token
		// 踩坑注意：ghcr.io 不包含 service 字段，有则传，无则只传 scope
		authURL := authInfo.Realm + "?scope=repository:" + url.QueryEscape(imagePath) + ":pull"
		if authInfo.Service != "" {
			authURL += "&service=" + url.QueryEscape(authInfo.Service)
		}

		tokReq, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
		if err != nil {
			return nil, err
		}
		tokReq.Header.Set("User-Agent", "RelayWatch/0.1 (OCI Client)")
		tokResp, err := client.Do(tokReq)
		if err != nil {
			return nil, fmt.Errorf("获取 OCI Token 失败: %w", err)
		}
		defer tokResp.Body.Close()

		var tokData struct {
			Token       string `json:"token"`
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(tokResp.Body).Decode(&tokData); err != nil {
			return nil, fmt.Errorf("解码 Token 失败: %w", err)
		}
		token = tokData.Token
		if token == "" {
			token = tokData.AccessToken
		}
	} else if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OCI registry 响应异常: HTTP %d", resp.StatusCode)
	}

	// 3. 请求 tag 列表
	listURL := fmt.Sprintf("%s://%s/v2/%s/tags/list", scheme, registryHost, imagePath)
	listReq, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, err
	}
	listReq.Header.Set("User-Agent", "RelayWatch/0.1 (OCI Client)")
	if token != "" {
		listReq.Header.Set("Authorization", "Bearer "+token)
	}
	listResp, err := client.Do(listReq)
	if err != nil {
		return nil, fmt.Errorf("查询 OCI tags 失败: %w", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode == http.StatusNotFound {
		return nil, &NotFoundError{Msg: fmt.Sprintf("镜像 %s 不存在", imagePath)}
	}
	if listResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询 tags 返回 HTTP %d", listResp.StatusCode)
	}

	var tagResp struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&tagResp); err != nil {
		return nil, fmt.Errorf("解码 tags 失败: %w", err)
	}
	return tagResp.Tags, nil
}

func (s *ContainerSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	rawImage := cfgString(cfg, "image")
	rawRegistry := cfgString(cfg, "registry")
	tagRegex := cfgString(cfg, "tag_regex")

	var registryHost string
	var imagePath string

	if rawRegistry != "" {
		registryHost = strings.TrimRight(strings.TrimPrefix(rawRegistry, "https://"), "/")
		imagePath = strings.Trim(rawImage, "/")
	} else {
		// 尝试从 image 解析域名，如 ghcr.io/owner/repo
		parts := strings.Split(rawImage, "/")
		if len(parts) > 1 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
			registryHost = parts[0]
			imagePath = strings.Join(parts[1:], "/")
		} else {
			// 默认 Docker Hub
			registryHost = "registry-1.docker.io"
			imagePath = strings.Trim(rawImage, "/")
			if !strings.Contains(imagePath, "/") {
				imagePath = "library/" + imagePath
			}
		}
	}

	pattern, err := compileRegex(tagRegex)
	if err != nil {
		return nil, fmt.Errorf("tag_regex 无效: %w", err)
	}

	tags, err := fetchOCITags(ctx, registryHost, imagePath)
	if err != nil {
		return nil, err
	}
	if len(tags) == 0 {
		return nil, &NotFoundError{Msg: fmt.Sprintf("镜像 %s/%s 没有可用的 tag", registryHost, imagePath)}
	}

	var best string
	for _, tag := range tags {
		if pattern != nil && !pattern.MatchString(tag) {
			continue
		}
		if best == "" || greaterVersion(tag, best) {
			best = tag
		}
	}

	if best == "" {
		return nil, fmt.Errorf("没有匹配 tag_regex 的 tag")
	}

	displayURL := fmt.Sprintf("https://%s/%s", registryHost, imagePath)
	if registryHost == "registry-1.docker.io" {
		displayURL = fmt.Sprintf("https://hub.docker.com/r/%s", imagePath)
	}

	return &Result{
		Version: best,
		URL:     displayURL,
	}, nil
}
