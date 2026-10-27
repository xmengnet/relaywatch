package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitTagSource(t *testing.T) {
	// 保存原函数并在测试后还原
	oldCmd := gitLsRemoteCmd
	defer func() { gitLsRemoteCmd = oldCmd }()

	ctx := context.Background()
	s := &GitTagSource{}

	// 1. Tag 模式测试
	gitLsRemoteCmd = func(ctx context.Context, args ...string) ([]byte, error) {
		out := "1111111111111111111111111111111111111111\trefs/tags/v1.0.0\n" +
			"2222222222222222222222222222222222222222\trefs/tags/v1.2.0\n" +
			"3333333333333333333333333333333333333333\trefs/tags/v1.1.5\n"
		return []byte(out), nil
	}

	res, err := s.Fetch(ctx, map[string]any{
		"url": "https://git.example.com/repo.git",
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "v1.2.0" {
		t.Errorf("got version %q, want v1.2.0", res.Version)
	}

	// 2. Commit 模式测试
	gitLsRemoteCmd = func(ctx context.Context, args ...string) ([]byte, error) {
		return []byte("abcdef1234567890abcdef1234567890abcdef12\tHEAD\n"), nil
	}

	resCommit, err := s.Fetch(ctx, map[string]any{
		"url":        "https://git.example.com/repo.git",
		"use_commit": true,
	})
	if err != nil {
		t.Fatalf("Fetch commit err: %v", err)
	}
	if resCommit.Version != "abcdef1234567890abcdef1234567890abcdef12" {
		t.Errorf("got %q", resCommit.Version)
	}
}

func TestHTTPHeaderSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "https://github.com/example/repo/releases/tag/v2.5.1")
			w.WriteHeader(http.StatusFound)
			return
		}
		if r.URL.Path == "/custom" {
			w.Header().Set("X-Custom-Version", "4.0.0")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &HTTPHeaderSource{}

	// 1. 抓取 Location 并提取正则
	res, err := s.Fetch(ctx, map[string]any{
		"url":   ts.URL + "/redirect",
		"regex": `tag/v?([0-9.]+)`,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "2.5.1" {
		t.Errorf("got %q, want 2.5.1", res.Version)
	}

	// 2. 自定义 Header
	resCustom, err := s.Fetch(ctx, map[string]any{
		"url":    ts.URL + "/custom",
		"header": "X-Custom-Version",
	})
	if err != nil {
		t.Fatalf("Fetch custom err: %v", err)
	}
	if resCustom.Version != "4.0.0" {
		t.Errorf("got %q, want 4.0.0", resCustom.Version)
	}

	// 3. 目标 Header 不存在
	_, err = s.Fetch(ctx, map[string]any{
		"url":    ts.URL + "/custom",
		"header": "Non-Existent-Header",
	})
	if _, ok := err.(*NotFoundError); !ok {
		t.Errorf("want NotFoundError, got %v", err)
	}
}

func TestBitbucketSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2.0/repositories/test/repo/refs/tags" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"values": [
				{"name": "0.9.0", "links": {"html": {"href": "https://bitbucket.org/test/repo/src/0.9.0"}}},
				{"name": "1.0.1", "links": {"html": {"href": "https://bitbucket.org/test/repo/src/1.0.1"}}},
				{"name": "1.0.0", "links": {"html": {"href": "https://bitbucket.org/test/repo/src/1.0.0"}}}
			]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &BitbucketSource{}

	res, err := s.Fetch(ctx, map[string]any{
		"repo":     "test/repo",
		"api_base": ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "1.0.1" {
		t.Errorf("got %q, want 1.0.1", res.Version)
	}
}

func TestPagureSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/0/test-repo/git/tags" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"total_tags": 3, "tags": ["0.1.0", "0.2.1", "0.2.0"]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	s := &PagureSource{}

	res, err := s.Fetch(ctx, map[string]any{
		"repo": "test-repo",
		"host": ts.URL,
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "0.2.1" {
		t.Errorf("got %q, want 0.2.1", res.Version)
	}
}
