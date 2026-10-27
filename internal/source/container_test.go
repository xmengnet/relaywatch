package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseWWWAuthenticate(t *testing.T) {
	// 1. 标准带 service
	hdr1 := `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`
	info1, err := parseWWWAuthenticate(hdr1)
	if err != nil {
		t.Fatalf("parseWWWAuthenticate failed: %v", err)
	}
	if info1.Realm != "https://auth.docker.io/token" || info1.Service != "registry.docker.io" {
		t.Errorf("got %+v", info1)
	}

	// 2. GHCR 风格（无 service）
	hdr2 := `Bearer realm="https://ghcr.io/token",scope="repository:user/repo:pull"`
	info2, err := parseWWWAuthenticate(hdr2)
	if err != nil {
		t.Fatalf("parseWWWAuthenticate failed: %v", err)
	}
	if info2.Realm != "https://ghcr.io/token" || info2.Service != "" {
		t.Errorf("got %+v", info2)
	}
}

func TestContainerSource(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/":
			authHdr := r.Header.Get("Authorization")
			if authHdr == "" {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test-registry"`, ts.URL))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"token": "test-mock-token"}`)
		case r.URL.Path == "/v2/myorg/myimage/tags/list":
			authHdr := r.Header.Get("Authorization")
			if authHdr != "Bearer test-mock-token" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"name": "myorg/myimage", "tags": ["1.0.0", "1.2.0", "1.1.0", "latest"]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &ContainerSource{}

	hostWithoutScheme := strings.TrimPrefix(ts.URL, "http://")
	res, err := s.Fetch(ctx, map[string]any{
		"registry":  hostWithoutScheme,
		"image":     "myorg/myimage",
		"tag_regex": `^[0-9.]+$`, // 排除 latest
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "1.2.0" {
		t.Errorf("got version %q, want 1.2.0", res.Version)
	}
}
