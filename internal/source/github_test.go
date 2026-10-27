package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubReleaseSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHdr := r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/repos/octocat/Hello-World/releases/latest":
			if authHdr != "Bearer secret-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{
				"tag_name": "v1.2.3",
				"html_url": "https://github.com/octocat/Hello-World/releases/tag/v1.2.3",
				"prerelease": false,
				"draft": false,
				"name": "Release 1.2.3"
			}`)
		case "/repos/octocat/Hello-World/releases":
			// include_prerelease 请求
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `[
				{
					"tag_name": "v2.0.0-draft",
					"html_url": "https://github.com/octocat/Hello-World/releases/tag/v2.0.0-draft",
					"prerelease": true,
					"draft": true,
					"name": "Draft 2.0"
				},
				{
					"tag_name": "v2.0.0-beta.1",
					"html_url": "https://github.com/octocat/Hello-World/releases/tag/v2.0.0-beta.1",
					"prerelease": true,
					"draft": false,
					"name": "Beta 2.0"
				},
				{
					"tag_name": "v1.2.3",
					"html_url": "https://github.com/octocat/Hello-World/releases/tag/v1.2.3",
					"prerelease": false,
					"draft": false,
					"name": "Release 1.2.3"
				}
			]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	SetSiteConfig([]MirrorRule{{From: "https://api.github.com", To: ts.URL}}, nil)
	defer SetSiteConfig(nil, nil)

	ctx := context.Background()
	s := &GitHubReleaseSource{}

	// 1. 参数校验
	if err := s.Validate(map[string]any{}); err == nil {
		t.Error("expected error for missing repo")
	}
	if err := s.Validate(map[string]any{"repo": "invalid-repo"}); err == nil {
		t.Error("expected error for repo without slash")
	}
	if err := s.Validate(map[string]any{"repo": "octocat/Hello-World"}); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}

	// 2. latest release 包含 token
	res, err := s.Fetch(ctx, map[string]any{
		"repo":  "octocat/Hello-World",
		"token": "secret-token",
	})
	if err != nil {
		t.Fatalf("Fetch latest error: %v", err)
	}
	if res.Version != "v1.2.3" {
		t.Errorf("got version %q, want v1.2.3", res.Version)
	}
	if res.URL != "https://github.com/octocat/Hello-World/releases/tag/v1.2.3" {
		t.Errorf("got url %q", res.URL)
	}
	if res.Extra["name"] != "Release 1.2.3" {
		t.Errorf("got extra name %v", res.Extra["name"])
	}

	// 3. include_prerelease 模式（跳过 draft，获取 beta）
	resPre, err := s.Fetch(ctx, map[string]any{
		"repo":               "octocat/Hello-World",
		"include_prerelease": true,
	})
	if err != nil {
		t.Fatalf("Fetch prerelease error: %v", err)
	}
	if resPre.Version != "v2.0.0-beta.1" {
		t.Errorf("got version %q, want v2.0.0-beta.1", resPre.Version)
	}
}

func TestGitHubTagSource(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/octocat/Hello-World/tags":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `[
				{"name": "v1.0.0", "commit": {"sha": "aaa"}},
				{"name": "v1.2.0", "commit": {"sha": "bbb"}},
				{"name": "v1.1.5", "commit": {"sha": "ccc"}},
				{"name": "v2.0.0-alpha", "commit": {"sha": "ddd"}}
			]`)
		case "/repos/octocat/Empty-Repo/tags":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	SetSiteConfig([]MirrorRule{{From: "https://api.github.com", To: ts.URL}}, nil)
	defer SetSiteConfig(nil, nil)

	ctx := context.Background()
	s := &GitHubTagSource{}

	// 1. 参数校验
	if err := s.Validate(map[string]any{}); err == nil {
		t.Error("expected error for missing repo")
	}
	if err := s.Validate(map[string]any{"repo": "x", "tag_pattern": "["}); err == nil {
		t.Error("expected error for invalid regex pattern")
	}

	// 2. 默认选择最大版本 (按 semver 比较，v1.2.0 vs v2.0.0-alpha，v2.0.0-alpha > v1.2.0)
	res, err := s.Fetch(ctx, map[string]any{
		"repo": "octocat/Hello-World",
	})
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if res.Version != "v2.0.0-alpha" {
		t.Errorf("got version %q, want v2.0.0-alpha", res.Version)
	}

	// 3. 正则过滤 tag_pattern: 仅稳定版 v1.x
	resFiltered, err := s.Fetch(ctx, map[string]any{
		"repo":        "octocat/Hello-World",
		"tag_pattern": `^v1\.[0-9.]+$`,
	})
	if err != nil {
		t.Fatalf("Fetch filtered error: %v", err)
	}
	if resFiltered.Version != "v1.2.0" {
		t.Errorf("got version %q, want v1.2.0", resFiltered.Version)
	}

	// 4. 空仓库 (返回 NotFoundError)
	_, err = s.Fetch(ctx, map[string]any{
		"repo": "octocat/Empty-Repo",
	})
	if _, ok := err.(*NotFoundError); !ok {
		t.Errorf("expected NotFoundError for empty tags, got: %v", err)
	}
}
