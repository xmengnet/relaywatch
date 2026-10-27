package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDebianPkgSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/src/ripgrep/" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"package": "ripgrep", "versions": [
				{"version": "14.1.0-2", "suite": "sid"},
				{"version": "13.0.0-4", "suite": "bookworm"}
			]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &DebianPkgSource{}

	// 1. 默认不剥离 release 后缀
	res, err := s.Fetch(ctx, map[string]any{
		"pkgname":  "ripgrep",
		"api_base": ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "14.1.0-2" {
		t.Errorf("got %q, want 14.1.0-2", res.Version)
	}

	// 2. 剥离 release 后缀
	resStrip, err := s.Fetch(ctx, map[string]any{
		"pkgname":       "ripgrep",
		"strip_release": true,
		"api_base":      ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch strip err: %v", err)
	}
	if resStrip.Version != "14.1.0" {
		t.Errorf("got %q, want 14.1.0", resStrip.Version)
	}
}

func TestUbuntuPkgSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/1.0/ubuntu/+archive/primary" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"entries": [
				{"status": "Published", "source_package_version": "8.5.0-2ubuntu10", "distro_series_link": "https://api.launchpad.net/1.0/ubuntu/noble"},
				{"status": "Deleted", "source_package_version": "8.4.0", "distro_series_link": "https://api.launchpad.net/1.0/ubuntu/mantic"}
			]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &UbuntuPkgSource{}

	// 剥离 release 后缀
	res, err := s.Fetch(ctx, map[string]any{
		"pkgname":       "curl",
		"suite":         "noble",
		"strip_release": true,
		"api_base":      ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "8.5.0" {
		t.Errorf("got %q, want 8.5.0", res.Version)
	}
}
