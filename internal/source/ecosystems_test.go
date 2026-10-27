package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRepologySource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/project/ripgrep" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `[
				{"repo": "debian_unstable", "subrepo": "", "version": "13.0.0"},
				{"repo": "arch", "subrepo": "extra", "version": "14.1.0"},
				{"repo": "arch", "subrepo": "extra", "version": "14.1.1"},
				{"repo": "aur", "subrepo": "", "version": "14.1.1"}
			]`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	s := &RepologySource{}
	ctx := context.Background()

	// 匹配 arch 仓库，取到最高版本 14.1.1
	res, err := s.Fetch(ctx, map[string]any{
		"project":  "ripgrep",
		"repo":     "arch",
		"subrepo":  "extra",
		"api_base": ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "14.1.1" {
		t.Errorf("got version %q, want 14.1.1", res.Version)
	}

	// 查不存在的项目
	_, err = s.Fetch(ctx, map[string]any{
		"project":  "not-found",
		"repo":     "arch",
		"api_base": ts.URL,
	})
	if _, ok := err.(*NotFoundError); !ok {
		t.Errorf("want NotFoundError, got %v", err)
	}
}

func TestAnityaSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/project/13815" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"id": 13815, "name": "ripgrep", "version": "14.1.1"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	s := &AnityaSource{}
	ctx := context.Background()

	res, err := s.Fetch(ctx, map[string]any{
		"project":  "13815",
		"api_base": ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "14.1.1" {
		t.Errorf("got %q, want 14.1.1", res.Version)
	}

	// 404
	_, err = s.Fetch(ctx, map[string]any{
		"project":  "0",
		"api_base": ts.URL,
	})
	if _, ok := err.(*NotFoundError); !ok {
		t.Errorf("want NotFoundError, got %v", err)
	}
}

func TestRubyGemsSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/versions/rails.json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `[
				{"number": "8.0.0.beta1", "prerelease": true},
				{"number": "7.1.3", "prerelease": false},
				{"number": "7.1.2", "prerelease": false}
			]`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	s := &RubyGemsSource{}
	ctx := context.Background()

	// 默认不包含预发布，应取到 7.1.3
	res, err := s.Fetch(ctx, map[string]any{
		"gem":      "rails",
		"api_base": ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "7.1.3" {
		t.Errorf("got %q, want 7.1.3", res.Version)
	}

	// 包含预发布，应取到 8.0.0.beta1
	resPre, err := s.Fetch(ctx, map[string]any{
		"gem":                "rails",
		"include_prerelease": true,
		"api_base":           ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch prerelease err: %v", err)
	}
	if resPre.Version != "8.0.0.beta1" {
		t.Errorf("got %q, want 8.0.0.beta1", resPre.Version)
	}
}

func TestMavenSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/org/apache/commons/commons-lang3/maven-metadata.xml" {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>org.apache.commons</groupId>
  <artifactId>commons-lang3</artifactId>
  <versioning>
    <latest>3.14.0</latest>
    <release>3.14.0</release>
    <versions>
      <version>3.12.0</version>
      <version>3.13.0</version>
      <version>3.14.0</version>
    </versions>
  </versioning>
</metadata>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	s := &MavenSource{}
	ctx := context.Background()

	res, err := s.Fetch(ctx, map[string]any{
		"group":    "org.apache.commons",
		"artifact": "commons-lang3",
		"repo":     ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "3.14.0" {
		t.Errorf("got %q, want 3.14.0", res.Version)
	}
}

func TestPackagistSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/packages/monolog/monolog.json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"package": {
				"name": "monolog/monolog",
				"versions": {
					"dev-main": {"version": "dev-main"},
					"3.6.0-alpha1": {"version": "3.6.0-alpha1"},
					"3.5.0": {"version": "3.5.0"},
					"2.9.2": {"version": "2.9.2"}
				}
			}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	s := &PackagistSource{}
	ctx := context.Background()

	// 默认排除 dev 与 prerelease，应为 3.5.0
	res, err := s.Fetch(ctx, map[string]any{
		"package":  "monolog/monolog",
		"api_base": ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "3.5.0" {
		t.Errorf("got %q, want 3.5.0", res.Version)
	}

	// 开启预发布，应为 3.6.0-alpha1
	resPre, err := s.Fetch(ctx, map[string]any{
		"package":            "monolog/monolog",
		"include_prerelease": true,
		"api_base":           ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if resPre.Version != "3.6.0-alpha1" {
		t.Errorf("got %q, want 3.6.0-alpha1", resPre.Version)
	}
}

func TestCPANSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/release/Mojolicious" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"name": "Mojolicious-9.36", "author": "SRI", "version": "9.36"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &CPANSource{}

	res, err := s.Fetch(ctx, map[string]any{
		"module":   "Mojolicious",
		"api_base": ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "9.36" {
		t.Errorf("got %q, want 9.36", res.Version)
	}
}

func TestCRANSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ggplot2" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"Package": "ggplot2", "Version": "3.5.0"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &CRANSource{}

	res, err := s.Fetch(ctx, map[string]any{
		"package":  "ggplot2",
		"api_base": ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "3.5.0" {
		t.Errorf("got %q, want 3.5.0", res.Version)
	}
}

func TestHackageSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/package/pandoc/preferred.json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"normal-version": ["3.1.11.1", "3.1.11"]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &HackageSource{}

	res, err := s.Fetch(ctx, map[string]any{
		"package":  "pandoc",
		"api_base": ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "3.1.11.1" {
		t.Errorf("got %q, want 3.1.11.1", res.Version)
	}
}
