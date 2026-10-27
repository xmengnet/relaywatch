package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAURSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rpc/v5/info" {
			pkg := r.URL.Query().Get("arg[]")
			w.Header().Set("Content-Type", "application/json")
			if pkg == "yay" {
				fmt.Fprintln(w, `{
					"version": 5,
					"type": "multiinfo",
					"resultcount": 1,
					"results": [
						{"Name": "yay", "Version": "12.3.5-1", "URL": "https://github.com/Jguer/yay"}
					]
				}`)
				return
			}
			// 不存在的包
			fmt.Fprintln(w, `{
				"version": 5,
				"type": "multiinfo",
				"resultcount": 0,
				"results": []
			}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	SetSiteConfig([]MirrorRule{{From: "https://aur.archlinux.org", To: ts.URL}}, nil)
	defer SetSiteConfig(nil, nil)

	ctx := context.Background()
	s := &AURSource{}

	// 1. 参数校验
	if err := s.Validate(map[string]any{}); err == nil {
		t.Error("expected error for missing pkgname")
	}

	// 2. 正常查询
	res, err := s.Fetch(ctx, map[string]any{"pkgname": "yay"})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "12.3.5-1" {
		t.Errorf("got %q, want 12.3.5-1", res.Version)
	}
	if res.URL != "https://aur.archlinux.org/packages/yay" {
		t.Errorf("got url %q", res.URL)
	}

	// 3. 不存在的包
	_, err = s.Fetch(ctx, map[string]any{"pkgname": "not-exists"})
	if _, ok := err.(*NotFoundError); !ok {
		t.Errorf("expected NotFoundError, got %v", err)
	}
}

func TestArchOfficialSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/packages/search/json/" {
			q := r.URL.Query().Get("q")
			repo := r.URL.Query().Get("repo")
			w.Header().Set("Content-Type", "application/json")
			if q == "linux" {
				if repo == "core" || repo == "" {
					fmt.Fprintln(w, `{
						"version": 2,
						"limit": 250,
						"valid": true,
						"results": [
							{"pkgname": "linux", "repo": "core", "pkgver": "6.8.9.arch1-1"}
						]
					}`)
					return
				}
			}
			fmt.Fprintln(w, `{
				"version": 2,
				"limit": 250,
				"valid": true,
				"results": []
			}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	SetSiteConfig([]MirrorRule{{From: "https://archlinux.org", To: ts.URL}}, nil)
	defer SetSiteConfig(nil, nil)

	ctx := context.Background()
	s := &ArchOfficialSource{}

	// 1. 参数校验
	if err := s.Validate(map[string]any{}); err == nil {
		t.Error("expected error for missing pkgname")
	}

	// 2. 正常查询（未指定 repo）
	res, err := s.Fetch(ctx, map[string]any{"pkgname": "linux"})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "6.8.9.arch1-1" {
		t.Errorf("got %q, want 6.8.9.arch1-1", res.Version)
	}
	if res.Extra["repo"] != "core" {
		t.Errorf("got repo extra %v", res.Extra["repo"])
	}

	// 3. 查指定 repo
	resRepo, err := s.Fetch(ctx, map[string]any{"pkgname": "linux", "repo": "core"})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if resRepo.Version != "6.8.9.arch1-1" {
		t.Errorf("got %q", resRepo.Version)
	}

	// 4. 查不存在的包
	_, err = s.Fetch(ctx, map[string]any{"pkgname": "linux", "repo": "extra"})
	if _, ok := err.(*NotFoundError); !ok {
		t.Errorf("expected NotFoundError, got %v", err)
	}
}
