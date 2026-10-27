package source

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// GitHubReleaseSource github_release 源：最新 Release（可含预发布）。
type GitHubReleaseSource struct{}

func (s *GitHubReleaseSource) Type() string { return "github_release" }

func (s *GitHubReleaseSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "GitHub Release",
		Icon:  "mdi-github",
		Fields: []FieldSpec{
			{Key: "repo", Label: "仓库", Type: "string", Required: true, Placeholder: "owner/name", Help: "如 egoist/mygo-cloudmusic"},
			{Key: "include_prerelease", Label: "包含预发布", Type: "bool"},
			{Key: "token", Label: "访问令牌", Type: "secret", Help: "公开仓库可留空，私有仓库或避免限流时填写"},
		},
	}
}

func (s *GitHubReleaseSource) Validate(cfg map[string]any) error {
	if err := required(cfg, "repo"); err != nil {
		return err
	}
	repo := cfgString(cfg, "repo")
	if strings.Count(repo, "/") != 1 {
		return fmt.Errorf("repo 需为 owner/name 格式")
	}
	return nil
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
	Name        string    `json:"name"`
}

func (s *GitHubReleaseSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	repo := cfgString(cfg, "repo")
	headers := map[string]string{}
	if tok := cfgString(cfg, "token"); tok != "" {
		headers["Authorization"] = "Bearer " + tok
	}
	if cfgBool(cfg, "include_prerelease") {
		// /latest 会过滤 prerelease 和 draft，取 releases 列表第一个非 draft
		var releases []ghRelease
		if err := fetchJSON(ctx, Options{}, "https://api.github.com/repos/"+repo+"/releases?per_page=10", headers, &releases); err != nil {
			return nil, err
		}
		for _, r := range releases {
			if r.Draft {
				continue
			}
			return &Result{Version: r.TagName, URL: r.HTMLURL, PublishedAt: r.PublishedAt, Extra: map[string]any{"name": r.Name}}, nil
		}
		return nil, fmt.Errorf("仓库没有可用 release")
	}
	var r ghRelease
	if err := fetchJSON(ctx, Options{}, "https://api.github.com/repos/"+repo+"/releases/latest", headers, &r); err != nil {
		return nil, err
	}
	return &Result{Version: r.TagName, URL: r.HTMLURL, PublishedAt: r.PublishedAt, Extra: map[string]any{"name": r.Name}}, nil
}

// GitHubTagSource github_tag 源：最大版本 tag。
type GitHubTagSource struct{}

func (s *GitHubTagSource) Type() string { return "github_tag" }

func (s *GitHubTagSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "GitHub Tag",
		Icon:  "mdi-tag-outline",
		Fields: []FieldSpec{
			{Key: "repo", Label: "仓库", Type: "string", Required: true, Placeholder: "owner/name"},
			{Key: "tag_pattern", Label: "Tag 正则", Type: "string", Help: "只考虑匹配的 tag，如 ^v\\d+\\.\\d+\\.\\d+$"},
			{Key: "token", Label: "访问令牌", Type: "secret"},
		},
	}
}

func (s *GitHubTagSource) Validate(cfg map[string]any) error {
	if err := required(cfg, "repo"); err != nil {
		return err
	}
	return ValidateVersionFilters(cfgString(cfg, "tag_pattern"), "")
}

type ghTag struct {
	Name string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
		URL string `json:"url"`
	} `json:"commit"`
}

func (s *GitHubTagSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	repo := cfgString(cfg, "repo")
	headers := map[string]string{}
	if tok := cfgString(cfg, "token"); tok != "" {
		headers["Authorization"] = "Bearer " + tok
	}
	var tags []ghTag
	if err := fetchJSON(ctx, Options{}, "https://api.github.com/repos/"+repo+"/tags?per_page=100", headers, &tags); err != nil {
		return nil, err
	}
	if len(tags) == 0 {
		return nil, &NotFoundError{Msg: "仓库没有 tag"}
	}
	pattern, err := compileRegex(cfgString(cfg, "tag_pattern"))
	if err != nil {
		return nil, fmt.Errorf("tag_pattern 无效: %w", err)
	}
	// 选择「最大」版本：semver 优先，否则分段比较
	best := ""
	bestURL := ""
	for _, t := range tags {
		name := t.Name
		if pattern != nil && !pattern.MatchString(name) {
			continue
		}
		if best == "" || greaterVersion(name, best) {
			best = name
			bestURL = "https://github.com/" + repo + "/releases/tag/" + name
		}
	}
	if best == "" {
		return nil, fmt.Errorf("没有匹配 tag_pattern 的 tag")
	}
	return &Result{Version: best, URL: bestURL}, nil
}

// greaterVersion 判断 a 是否比 b 更高（先用 semver，回退分段比较）。
func greaterVersion(a, b string) bool {
	c, err := CompareVersions(a, b)
	if err != nil {
		return false
	}
	return c > 0
}
