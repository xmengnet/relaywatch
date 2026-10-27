package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitLabReleaseSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("PRIVATE-TOKEN")
		if token != "my-gl-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		uri := r.URL.RequestURI()
		switch {
		case uri == "/api/v4/projects/group%2Fproject/releases/permalink/latest" || r.URL.Path == "/api/v4/projects/group/project/releases/permalink/latest":
			fmt.Fprintln(w, `{"tag_name": "v1.5.0", "web_url": "https://gitlab.com/group/project/-/releases/v1.5.0"}`)
		case uri == "/api/v4/projects/fallback%2Fproject/releases/permalink/latest" || r.URL.Path == "/api/v4/projects/fallback/project/releases/permalink/latest":
			http.NotFound(w, r)
		case uri == "/api/v4/projects/fallback%2Fproject/releases?per_page=1" || r.URL.Path == "/api/v4/projects/fallback/project/releases":
			fmt.Fprintln(w, `[{"tag_name": "v0.9.0", "web_url": "https://gitlab.com/fallback/project/-/releases/v0.9.0"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &GitLabReleaseSource{}

	// 1. 必填校验
	if err := s.Validate(map[string]any{"host": ts.URL}); err == nil {
		t.Error("expected error for missing project")
	}

	// 2. permalink/latest 成功
	res, err := s.Fetch(ctx, map[string]any{
		"host":    ts.URL,
		"project": "group/project",
		"token":   "my-gl-token",
	})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "v1.5.0" {
		t.Errorf("got %q, want v1.5.0", res.Version)
	}

	// 3. 回退 releases 列表
	resFallback, err := s.Fetch(ctx, map[string]any{
		"host":    ts.URL,
		"project": "fallback/project",
		"token":   "my-gl-token",
	})
	if err != nil {
		t.Fatalf("Fetch fallback error: %v", err)
	}
	if resFallback.Version != "v0.9.0" {
		t.Errorf("got %q, want v0.9.0", resFallback.Version)
	}
}

func TestGiteaReleaseSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "token my-gitea-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/v1/repos/owner/repo/releases/latest" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"tag_name": "v2.1.0", "html_url": "https://gitea.com/owner/repo/releases/tag/v2.1.0"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &GiteaReleaseSource{}

	if err := s.Validate(map[string]any{"host": ts.URL}); err == nil {
		t.Error("expected error for missing repo")
	}

	res, err := s.Fetch(ctx, map[string]any{
		"host":  ts.URL,
		"repo":  "owner/repo",
		"token": "my-gitea-token",
	})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "v2.1.0" {
		t.Errorf("got %q, want v2.1.0", res.Version)
	}
}

func TestPyPISource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pypi/requests/json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"info": {"version": "2.31.0", "project_url": "https://pypi.org/project/requests/"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	SetSiteConfig([]MirrorRule{{From: "https://pypi.org", To: ts.URL}}, nil)
	defer SetSiteConfig(nil, nil)

	ctx := context.Background()
	s := &PyPISource{}

	if err := s.Validate(map[string]any{}); err == nil {
		t.Error("expected error for missing project")
	}

	res, err := s.Fetch(ctx, map[string]any{"project": "requests"})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "2.31.0" {
		t.Errorf("got %q, want 2.31.0", res.Version)
	}
	if res.URL != "https://pypi.org/project/requests/" {
		t.Errorf("got url %q", res.URL)
	}
}

func TestNPMSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/vite/latest":
			fmt.Fprintln(w, `{"version": "5.2.0"}`)
		case "/vite/beta":
			fmt.Fprintln(w, `{"version": "5.3.0-beta.1"}`)
		case "/@vue/core/latest":
			fmt.Fprintln(w, `{"version": "3.4.21"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	SetSiteConfig([]MirrorRule{{From: "https://registry.npmjs.org", To: ts.URL}}, nil)
	defer SetSiteConfig(nil, nil)

	ctx := context.Background()
	s := &NPMSource{}

	if err := s.Validate(map[string]any{}); err == nil {
		t.Error("expected error for missing package")
	}

	// 1. 普通包默认 latest tag
	res, err := s.Fetch(ctx, map[string]any{"package": "vite"})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "5.2.0" {
		t.Errorf("got %q, want 5.2.0", res.Version)
	}

	// 2. 指定 tag
	resBeta, err := s.Fetch(ctx, map[string]any{"package": "vite", "dist_tag": "beta"})
	if err != nil {
		t.Fatalf("Fetch beta error: %v", err)
	}
	if resBeta.Version != "5.3.0-beta.1" {
		t.Errorf("got %q, want 5.3.0-beta.1", resBeta.Version)
	}

	// 3. scoped 包 (@vue/core)
	resScoped, err := s.Fetch(ctx, map[string]any{"package": "@vue/core"})
	if err != nil {
		t.Fatalf("Fetch scoped error: %v", err)
	}
	if resScoped.Version != "3.4.21" {
		t.Errorf("got %q, want 3.4.21", resScoped.Version)
	}
}

func TestCratesSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/crates/ripgrep" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"crate": {"max_version": "14.1.0", "repository": "https://github.com/BurntSushi/ripgrep"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	SetSiteConfig([]MirrorRule{{From: "https://crates.io", To: ts.URL}}, nil)
	defer SetSiteConfig(nil, nil)

	ctx := context.Background()
	s := &CratesSource{}

	if err := s.Validate(map[string]any{}); err == nil {
		t.Error("expected error for missing crate")
	}

	res, err := s.Fetch(ctx, map[string]any{"crate": "ripgrep"})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "14.1.0" {
		t.Errorf("got %q, want 14.1.0", res.Version)
	}
	if res.URL != "https://github.com/BurntSushi/ripgrep" {
		t.Errorf("got url %q", res.URL)
	}
}

func TestGoModuleSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/github.com/gin-gonic/gin/@latest" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"Version": "v1.9.1"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	SetSiteConfig([]MirrorRule{{From: "https://proxy.golang.org", To: ts.URL}}, nil)
	defer SetSiteConfig(nil, nil)

	ctx := context.Background()
	s := &GoModuleSource{}

	if err := s.Validate(map[string]any{}); err == nil {
		t.Error("expected error for missing module")
	}

	res, err := s.Fetch(ctx, map[string]any{"module": "github.com/gin-gonic/gin"})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "v1.9.1" {
		t.Errorf("got %q, want v1.9.1", res.Version)
	}
	if res.URL != "https://pkg.go.dev/github.com/gin-gonic/gin@v1.9.1" {
		t.Errorf("got url %q", res.URL)
	}
}
