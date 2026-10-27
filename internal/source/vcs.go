package source

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// gitLsRemoteCmd 声明执行 git ls-remote 的函数签名，测试可注入替换。
var gitLsRemoteCmd = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"ls-remote"}, args...)...)
	return cmd.Output()
}

// ---------- git_tag (通用 Git 源) ----------

// GitTagSource git_tag 源：免 Clone 远程直接查询 tags 或 branch HEAD。
type GitTagSource struct{}

func (s *GitTagSource) Type() string { return "git_tag" }

func (s *GitTagSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Git (通用仓库)",
		Icon:  "mdi-git",
		Fields: []FieldSpec{
			{Key: "url", Label: "仓库 URL", Type: "string", Required: true, Placeholder: "https://git.example.com/repo.git", Help: "支持 https:// 或 git:// 链接"},
			{Key: "branch", Label: "分支名称", Type: "string", Help: "可选；填写则查询该分支最新 commit SHA"},
			{Key: "use_commit", Label: "跟踪 Commit", Type: "bool", Help: "跟踪分支或默认 HEAD 的 Commit SHA 而非 Tag"},
			{Key: "tag_pattern", Label: "Tag 正则", Type: "string", Help: "仅考虑匹配的 tag，如 ^v[0-9.]+$"},
		},
	}
}

func (s *GitTagSource) Validate(cfg map[string]any) error {
	if err := required(cfg, "url"); err != nil {
		return err
	}
	return ValidateVersionFilters(cfgString(cfg, "tag_pattern"), "")
}

func (s *GitTagSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	repoURL := cfgString(cfg, "url")
	branch := cfgString(cfg, "branch")
	useCommit := cfgBool(cfg, "use_commit")
	tagPattern := cfgString(cfg, "tag_pattern")

	if useCommit || branch != "" {
		ref := "HEAD"
		if branch != "" {
			ref = "refs/heads/" + branch
		}
		out, err := gitLsRemoteCmd(ctx, repoURL, ref)
		if err != nil {
			return nil, fmt.Errorf("git ls-remote 失败: %w", err)
		}
		scanner := bufio.NewScanner(bytes.NewReader(out))
		if scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) > 0 {
				sha := fields[0]
				return &Result{
					Version: sha,
					URL:     repoURL,
					Extra:   map[string]any{"ref": ref, "sha": sha},
				}, nil
			}
		}
		return nil, &NotFoundError{Msg: fmt.Sprintf("分支 %s 不存在或仓库为空", ref)}
	}

	// Tag 模式
	pattern, err := compileRegex(tagPattern)
	if err != nil {
		return nil, fmt.Errorf("tag_pattern 无效: %w", err)
	}

	out, err := gitLsRemoteCmd(ctx, "--tags", "--refs", repoURL)
	if err != nil {
		return nil, fmt.Errorf("git ls-remote --tags 失败: %w", err)
	}

	var best string
	var bestSHA string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\trefs/tags/")
		if len(parts) != 2 {
			continue
		}
		sha := strings.TrimSpace(parts[0])
		tag := strings.TrimSpace(parts[1])
		if pattern != nil && !pattern.MatchString(tag) {
			continue
		}
		if best == "" || greaterVersion(tag, best) {
			best = tag
			bestSHA = sha
		}
	}

	if best == "" {
		return nil, &NotFoundError{Msg: "仓库没有找到匹配的 tag"}
	}

	return &Result{
		Version: best,
		URL:     repoURL,
		Extra:   map[string]any{"sha": bestSHA},
	}, nil
}

// ---------- Bitbucket ----------

// BitbucketSource bitbucket 源。
type BitbucketSource struct{}

func (s *BitbucketSource) Type() string { return "bitbucket" }

func (s *BitbucketSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Bitbucket",
		Icon:  "mdi-bitbucket",
		Fields: []FieldSpec{
			{Key: "repo", Label: "仓库", Type: "string", Required: true, Placeholder: "workspace/repo"},
			{Key: "branch", Label: "分支", Type: "string", Help: "可选；填写则查询该分支最新 Commit"},
			{Key: "tag_pattern", Label: "Tag 正则", Type: "string"},
			{Key: "token", Label: "访问令牌", Type: "secret", Help: "私有仓库填写"},
		},
	}
}

func (s *BitbucketSource) Validate(cfg map[string]any) error {
	if err := required(cfg, "repo"); err != nil {
		return err
	}
	return ValidateVersionFilters(cfgString(cfg, "tag_pattern"), "")
}

type bitbucketTagItem struct {
	Name  string `json:"name"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

type bitbucketTagsResponse struct {
	Values []bitbucketTagItem `json:"values"`
}

type bitbucketCommitsResponse struct {
	Values []struct {
		Hash string `json:"hash"`
		Date string `json:"date"`
	} `json:"values"`
}

func (s *BitbucketSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	repo := cfgString(cfg, "repo")
	branch := cfgString(cfg, "branch")
	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://api.bitbucket.org"
	}
	headers := map[string]string{}
	if tok := cfgString(cfg, "token"); tok != "" {
		headers["Authorization"] = "Bearer " + tok
	}

	// 分支 Commit 模式
	if branch != "" {
		u := fmt.Sprintf("%s/2.0/repositories/%s/commits/%s?pagelen=1", base, repo, url.PathEscape(branch))
		var resp bitbucketCommitsResponse
		if err := fetchJSON(ctx, Options{}, u, headers, &resp); err != nil {
			return nil, err
		}
		if len(resp.Values) == 0 {
			return nil, &NotFoundError{Msg: fmt.Sprintf("分支 %s 无 commit", branch)}
		}
		c := resp.Values[0]
		short := c.Hash
		if len(short) > 12 {
			short = short[:12]
		}
		return &Result{
			Version: short,
			URL:     fmt.Sprintf("https://bitbucket.org/%s/commits/%s", repo, c.Hash),
			Extra:   map[string]any{"hash": c.Hash, "date": c.Date},
		}, nil
	}

	// Tag 模式
	pattern, err := compileRegex(cfgString(cfg, "tag_pattern"))
	if err != nil {
		return nil, fmt.Errorf("tag_pattern 无效: %w", err)
	}

	u := fmt.Sprintf("%s/2.0/repositories/%s/refs/tags?pagelen=100", base, repo)
	var resp bitbucketTagsResponse
	if err := fetchJSON(ctx, Options{}, u, headers, &resp); err != nil {
		return nil, err
	}
	if len(resp.Values) == 0 {
		return nil, &NotFoundError{Msg: fmt.Sprintf("仓库 %s 没有 tag", repo)}
	}

	var best string
	var bestURL string
	for _, item := range resp.Values {
		if pattern != nil && !pattern.MatchString(item.Name) {
			continue
		}
		if best == "" || greaterVersion(item.Name, best) {
			best = item.Name
			bestURL = item.Links.HTML.Href
		}
	}

	if best == "" {
		return nil, fmt.Errorf("没有匹配 tag_pattern 的 tag")
	}

	if bestURL == "" {
		bestURL = fmt.Sprintf("https://bitbucket.org/%s/src/%s", repo, best)
	}

	return &Result{
		Version: best,
		URL:     bestURL,
	}, nil
}

// ---------- Pagure ----------

// PagureSource pagure 源。
type PagureSource struct{}

func (s *PagureSource) Type() string { return "pagure" }

func (s *PagureSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Pagure",
		Icon:  "mdi-source-repository",
		Fields: []FieldSpec{
			{Key: "repo", Label: "仓库名称", Type: "string", Required: true, Placeholder: "pagure"},
			{Key: "host", Label: "站点域名", Type: "string", Placeholder: "https://pagure.io", Help: "默认使用 https://pagure.io"},
			{Key: "tag_pattern", Label: "Tag 正则", Type: "string"},
		},
	}
}

func (s *PagureSource) Validate(cfg map[string]any) error {
	if err := required(cfg, "repo"); err != nil {
		return err
	}
	return ValidateVersionFilters(cfgString(cfg, "tag_pattern"), "")
}

type pagureTagsResponse struct {
	TotalTags int      `json:"total_tags"`
	Tags      []string `json:"tags"`
}

func (s *PagureSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	repo := cfgString(cfg, "repo")
	host := strings.TrimRight(cfgString(cfg, "host"), "/")
	if host == "" {
		host = "https://pagure.io"
	}
	pattern, err := compileRegex(cfgString(cfg, "tag_pattern"))
	if err != nil {
		return nil, fmt.Errorf("tag_pattern 无效: %w", err)
	}

	u := fmt.Sprintf("%s/api/0/%s/git/tags", host, repo)
	var resp pagureTagsResponse
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.Tags) == 0 {
		return nil, &NotFoundError{Msg: fmt.Sprintf("Pagure 仓库 %s 查无 tag", repo)}
	}

	var best string
	for _, tag := range resp.Tags {
		if pattern != nil && !pattern.MatchString(tag) {
			continue
		}
		if best == "" || greaterVersion(tag, best) {
			best = tag
		}
	}

	if best == "" {
		return nil, fmt.Errorf("没有匹配 tag_pattern 的 tag")
	}

	return &Result{
		Version: best,
		URL:     fmt.Sprintf("%s/%s", host, repo),
	}, nil
}
