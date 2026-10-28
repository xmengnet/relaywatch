package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPJSONSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "relay-watch" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{
			"data": {
				"app": "my-service",
				"releases": [
					{"ver": "1.0.0"},
					{"ver": "2.1.0"}
				],
				"count": 42
			}
		}`)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &HTTPJSONSource{}

	// 1. 必填校验
	if err := s.Validate(map[string]any{"url": ts.URL}); err == nil {
		t.Error("expected error for missing path")
	}

	// 2. 点路径 + 数组下标读取
	res, err := s.Fetch(ctx, map[string]any{
		"url":     ts.URL,
		"path":    "data.releases.1.ver",
		"headers": "X-Custom: relay-watch",
	})
	if err != nil {
		t.Fatalf("Fetch array error: %v", err)
	}
	if res.Version != "2.1.0" {
		t.Errorf("got %q, want 2.1.0", res.Version)
	}

	// 3. 数字字段读取
	resNum, err := s.Fetch(ctx, map[string]any{
		"url":     ts.URL,
		"path":    "data.count",
		"headers": "X-Custom: relay-watch",
	})
	if err != nil {
		t.Fatalf("Fetch number error: %v", err)
	}
	if resNum.Version != "42" {
		t.Errorf("got %q, want 42", resNum.Version)
	}

	// 4. 路径不存在
	_, err = s.Fetch(ctx, map[string]any{
		"url":     ts.URL,
		"path":    "data.not_exists",
		"headers": "X-Custom: relay-watch",
	})
	if err == nil {
		t.Error("expected error for non-existent path")
	}
}

func TestHTTPRegexSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, `<html><body>
			<div>Current stable release: version="3.8.12" build 2026</div>
		</body></html>`)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &HTTPRegexSource{}

	// 1. 必填校验
	if err := s.Validate(map[string]any{"url": ts.URL}); err == nil {
		t.Error("expected error for missing regex")
	}
	if err := s.Validate(map[string]any{"url": ts.URL, "regex": "["}); err == nil {
		t.Error("expected error for invalid regex")
	}

	// 2. 正常提取默认分组 1
	res, err := s.Fetch(ctx, map[string]any{
		"url":   ts.URL,
		"regex": `version="([0-9.]+)"`,
	})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "3.8.12" {
		t.Errorf("got %q, want 3.8.12", res.Version)
	}

	// 3. 自定义捕获组
	resGroup, err := s.Fetch(ctx, map[string]any{
		"url":   ts.URL,
		"regex": `(version)="([0-9.]+)"`,
		"group": "2",
	})
	if err != nil {
		t.Fatalf("Fetch group error: %v", err)
	}
	if resGroup.Version != "3.8.12" {
		t.Errorf("got %q, want 3.8.12", resGroup.Version)
	}

	// 4. 不匹配返回错误
	_, err = s.Fetch(ctx, map[string]any{
		"url":   ts.URL,
		"regex": `non_existent_prefix_([0-9.]+)`,
	})
	if err == nil {
		t.Error("expected error when regex does not match")
	}
}

func TestHTTPHeaderSourceExtra(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			w.Header().Set("Location", "https://github.com/owner/repo/releases/tag/v4.5.6")
			w.WriteHeader(http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &HTTPHeaderSource{}

	if err := s.Validate(map[string]any{}); err == nil {
		t.Error("expected error for missing url")
	}

	// 1. HEAD 请求捕获 Location 头并用正则提取版本
	res, err := s.Fetch(ctx, map[string]any{
		"url":    ts.URL + "/latest",
		"header": "Location",
		"regex":  `tag/v?([0-9.]+)`,
		"group":  "1",
		"method": "HEAD",
	})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "4.5.6" {
		t.Errorf("got %q, want 4.5.6", res.Version)
	}

	// 2. 头部缺失返回 NotFoundError
	_, err = s.Fetch(ctx, map[string]any{
		"url":    ts.URL + "/latest",
		"header": "X-Non-Existent",
	})
	if _, ok := err.(*NotFoundError); !ok {
		t.Errorf("expected NotFoundError for missing header, got: %v", err)
	}
}

func TestDockerTagSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v2/repositories/library/nginx/tags" {
			page := r.URL.Query().Get("page")
			if page == "2" {
				fmt.Fprintln(w, `{
					"results": [
						{"name": "1.25.4"},
						{"name": "1.26.0"}
					],
					"next": ""
				}`)
				return
			}
			// page 1
			nextURL := fmt.Sprintf("https://hub.docker.com/v2/repositories/library/nginx/tags?page=2")
			fmt.Fprintf(w, `{
				"results": [
					{"name": "latest"},
					{"name": "1.25.3"},
					{"name": "alpine"}
				],
				"next": "%s"
			}`, nextURL)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	SetSiteConfig([]MirrorRule{{From: "https://hub.docker.com", To: ts.URL}}, nil)
	defer SetSiteConfig(nil, nil)

	ctx := context.Background()
	s := &DockerTagSource{}

	if err := s.Validate(map[string]any{}); err == nil {
		t.Error("expected error for missing image")
	}

	// 1. 自动补全 library/ 且跨页扫描选出最大 semver 版本
	res, err := s.Fetch(ctx, map[string]any{
		"image":     "nginx",
		"tag_regex": `^[0-9.]+$`, // 排除 latest/alpine
	})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "1.26.0" {
		t.Errorf("got %q, want 1.26.0", res.Version)
	}
	if res.URL != "https://hub.docker.com/r/library/nginx" {
		t.Errorf("got url %q", res.URL)
	}
}

func TestDockerTagSourceRegistry(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/":
			// OCI 探针：要求 Bearer 鉴权
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test-registry"`, ts.URL))
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"token": "test-mock-token"}`)
		case r.URL.Path == "/v2/myorg/myimage/tags/list":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"name": "myorg/myimage", "tags": ["1.0.0", "1.2.0", "1.1.0", "latest"]}`)
		case r.URL.Path == "/v2/repositories/library/nginx/tags":
			// Docker Hub 分页接口（被镜像规则重定向到本测试服务器）
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"results": [{"name": "1.30.0"}, {"name": "latest"}], "next": ""}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	host := ts.Listener.Addr().String()
	ctx := context.Background()
	s := &DockerTagSource{}

	// 1. registry 字段生效：非 Docker Hub 走标准 OCI tags API
	res, err := s.Fetch(ctx, map[string]any{
		"registry":  host,
		"image":     "myorg/myimage",
		"tag_regex": `^[0-9.]+$`, // 排除 latest
	})
	if err != nil {
		t.Fatalf("registry fetch error: %v", err)
	}
	if res.Version != "1.2.0" {
		t.Errorf("got %q, want 1.2.0", res.Version)
	}
	if want := "https://" + host + "/myorg/myimage"; res.URL != want {
		t.Errorf("got url %q, want %q", res.URL, want)
	}

	// 2. registry 带 scheme / 尾部斜杠也能识别
	if res, err = s.Fetch(ctx, map[string]any{
		"registry": ts.URL + "/",
		"image":    "myorg/myimage",
	}); err != nil || res.Version != "1.2.0" {
		t.Errorf("scheme-prefixed registry: res %v, err %v", res, err)
	}

	// 3. 不填 registry、镜像名自带域名：同样走 OCI，且域名被剥离
	if res, err = s.Fetch(ctx, map[string]any{
		"image": host + "/myorg/myimage",
	}); err != nil || res.Version != "1.2.0" {
		t.Errorf("image with domain: res %v, err %v", res, err)
	}

	// 4. Docker Hub 别名（docker.io）仍走 hub.docker.com 分页接口
	SetSiteConfig([]MirrorRule{{From: "https://hub.docker.com", To: ts.URL}}, nil)
	defer SetSiteConfig(nil, nil)
	res, err = s.Fetch(ctx, map[string]any{"registry": "docker.io", "image": "nginx", "tag_regex": `^[0-9.]+$`})
	if err != nil {
		t.Fatalf("docker.io alias error: %v", err)
	}
	if res.Version != "1.30.0" {
		t.Errorf("got %q, want 1.30.0", res.Version)
	}
	if res.URL != "https://hub.docker.com/r/library/nginx" {
		t.Errorf("got url %q", res.URL)
	}
}
