package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenVSXSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/rust-lang/rust-analyzer" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"version": "0.4.1876", "url": "https://open-vsx.org/extension/rust-lang/rust-analyzer/0.4.1876"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &OpenVSXSource{}

	res, err := s.Fetch(ctx, map[string]any{
		"extension": "rust-lang.rust-analyzer",
		"api_base":  ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "0.4.1876" {
		t.Errorf("got %q, want 0.4.1876", res.Version)
	}
}

func TestVSMarketplaceSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_apis/public/gallery/extensionquery" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"results": [{
				"extensions": [{
					"versions": [{"version": "0.41.0"}]
				}]
			}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &VSMarketplaceSource{}

	res, err := s.Fetch(ctx, map[string]any{
		"extension": "golang.go",
		"api_base":  ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "0.41.0" {
		t.Errorf("got %q, want 0.41.0", res.Version)
	}
}

func TestSnapcraftSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/snaps/info/canonical-livepatch" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"channel-map": [
				{"channel": {"name": "candidate"}, "version": "10.8.2"},
				{"channel": {"name": "stable"}, "version": "10.8.1"}
			]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &SnapcraftSource{}

	res, err := s.Fetch(ctx, map[string]any{
		"snap":     "canonical-livepatch",
		"channel":  "stable",
		"api_base": ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "10.8.1" {
		t.Errorf("got %q, want 10.8.1", res.Version)
	}
}
