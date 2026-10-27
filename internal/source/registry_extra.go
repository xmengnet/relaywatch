package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// ---------- GitLab ----------

// GitLabReleaseSource gitlab_release 源：自建/GitLab.com 最新 Release。
type GitLabReleaseSource struct{}

func (s *GitLabReleaseSource) Type() string { return "gitlab_release" }

func (s *GitLabReleaseSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "GitLab Release",
		Icon:  "mdi-gitlab",
		Fields: []FieldSpec{
			{Key: "host", Label: "GitLab 地址", Type: "string", Required: true, Placeholder: "https://gitlab.com"},
			{Key: "project", Label: "项目", Type: "string", Required: true, Placeholder: "group/project 或数字 ID"},
			{Key: "token", Label: "访问令牌", Type: "secret", Help: "私有项目必填"},
		},
	}
}

func (s *GitLabReleaseSource) Validate(cfg map[string]any) error {
	return required(cfg, "host", "project")
}

type gitlabRelease struct {
	TagName string `json:"tag_name"`
	WebURL  string `json:"web_url"`
}

func (s *GitLabReleaseSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	host := strings.TrimRight(cfgString(cfg, "host"), "/")
	project := url.PathEscape(cfgString(cfg, "project"))
	headers := map[string]string{}
	if tok := cfgString(cfg, "token"); tok != "" {
		headers["PRIVATE-TOKEN"] = tok
	}
	var rel gitlabRelease
	u := fmt.Sprintf("%s/api/v4/projects/%s/releases/permalink/latest", host, project)
	if err := fetchJSON(ctx, Options{}, u, headers, &rel); err != nil {
		// permalink/latest 需要 GitLab 15.7+，回退列表接口
		var list []gitlabRelease
		u2 := fmt.Sprintf("%s/api/v4/projects/%s/releases?per_page=1", host, project)
		if err2 := fetchJSON(ctx, Options{}, u2, headers, &list); err2 != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, &NotFoundError{Msg: "项目没有 release"}
		}
		rel = list[0]
	}
	return &Result{Version: rel.TagName, URL: rel.WebURL}, nil
}

// ---------- Gitea ----------

// GiteaReleaseSource gitea_release 源：Gitea / Forgejo。
type GiteaReleaseSource struct{}

func (s *GiteaReleaseSource) Type() string { return "gitea_release" }

func (s *GiteaReleaseSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Gitea / Forgejo",
		Icon:  "mdi-forge",
		Fields: []FieldSpec{
			{Key: "host", Label: "站点地址", Type: "string", Required: true, Placeholder: "https://gitea.example.com"},
			{Key: "repo", Label: "仓库", Type: "string", Required: true, Placeholder: "owner/name"},
			{Key: "token", Label: "访问令牌", Type: "secret"},
		},
	}
}

func (s *GiteaReleaseSource) Validate(cfg map[string]any) error {
	return required(cfg, "host", "repo")
}

type giteaRelease struct {
	TagName   string `json:"tag_name"`
	HTMLURL   string `json:"html_url"`
	Prerelease bool  `json:"prerelease"`
}

func (s *GiteaReleaseSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	host := strings.TrimRight(cfgString(cfg, "host"), "/")
	repo := cfgString(cfg, "repo")
	headers := map[string]string{}
	if tok := cfgString(cfg, "token"); tok != "" {
		headers["Authorization"] = "token " + tok
	}
	var rel giteaRelease
	u := fmt.Sprintf("%s/api/v1/repos/%s/releases/latest", host, repo)
	if err := fetchJSON(ctx, Options{}, u, headers, &rel); err != nil {
		return nil, err
	}
	return &Result{Version: rel.TagName, URL: rel.HTMLURL}, nil
}

// ---------- PyPI ----------

// PyPISource pypi 源。
type PyPISource struct{}

func (s *PyPISource) Type() string { return "pypi" }

func (s *PyPISource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "PyPI",
		Icon:  "mdi-language-python",
		Fields: []FieldSpec{
			{Key: "project", Label: "项目名", Type: "string", Required: true, Placeholder: "requests"},
		},
	}
}

func (s *PyPISource) Validate(cfg map[string]any) error {
	return required(cfg, "project")
}

type pypiJSON struct {
	Info struct {
		Version string `json:"version"`
		Home    string `json:"home_page"`
		ProjectURL string `json:"project_url"`
	} `json:"info"`
	Releases map[string][]struct{} `json:"releases"`
}

func (s *PyPISource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	project := cfgString(cfg, "project")
	var p pypiJSON
	u := fmt.Sprintf("https://pypi.org/pypi/%s/json", url.PathEscape(project))
	if err := fetchJSON(ctx, Options{}, u, nil, &p); err != nil {
		return nil, err
	}
	ver := p.Info.Version
	pageURL := p.Info.ProjectURL
	if pageURL == "" {
		pageURL = fmt.Sprintf("https://pypi.org/project/%s/", project)
	}
	return &Result{Version: ver, URL: pageURL}, nil
}

// ---------- npm ----------

// NPMSource npm 源（dist-tag，默认 latest）。
type NPMSource struct{}

func (s *NPMSource) Type() string { return "npm" }

func (s *NPMSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "npm",
		Icon:  "mdi-npm",
		Fields: []FieldSpec{
			{Key: "package", Label: "包名", Type: "string", Required: true, Placeholder: "vite"},
			{Key: "dist_tag", Label: "Dist Tag", Type: "string", Help: "默认 latest，可指定 next/beta 等"},
		},
	}
}

func (s *NPMSource) Validate(cfg map[string]any) error {
	return required(cfg, "package")
}

func (s *NPMSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	pkg := cfgString(cfg, "package")
	tag := cfgString(cfg, "dist_tag")
	if tag == "" {
		tag = "latest"
	}
	// registry 的包名可含 scope，直接拼 URL
	u := fmt.Sprintf("https://registry.npmjs.org/%s/%s", strings.TrimPrefix(pkg, "@"), tag)
	// scoped 包 URL 形如 @scope/name/latest，上面的 TrimPrefix 破坏了 scope；用正确编码重拼
	if strings.HasPrefix(pkg, "@") {
		u = fmt.Sprintf("https://registry.npmjs.org/%s/%s", pkg, tag)
	}
	var resp struct {
		Version string `json:"version"`
	}
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}
	return &Result{
		Version: resp.Version,
		URL:     fmt.Sprintf("https://www.npmjs.com/package/%s", pkg),
	}, nil
}

// ---------- crates.io ----------

// CratesSource crates.io 源。
type CratesSource struct{}

func (s *CratesSource) Type() string { return "crates" }

func (s *CratesSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "crates.io",
		Icon:  "mdi-language-rust",
		Fields: []FieldSpec{
			{Key: "crate", Label: "Crate 名", Type: "string", Required: true, Placeholder: "ripgrep"},
		},
	}
}

func (s *CratesSource) Validate(cfg map[string]any) error {
	return required(cfg, "crate")
}

type cratesResp struct {
	Crate struct {
		MaxVersion string `json:"max_version"`
		Homepage   string `json:"homepage"`
		Repository string `json:"repository"`
	} `json:"crate"`
}

func (s *CratesSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	crate := cfgString(cfg, "crate")
	var resp cratesResp
	u := fmt.Sprintf("https://crates.io/api/v1/crates/%s", url.PathEscape(crate))
	// crates.io 要求自定义 UA
	headers := map[string]string{"User-Agent": "RelayWatch/0.1 (version monitor)"}
	if err := fetchJSON(ctx, Options{}, u, headers, &resp); err != nil {
		return nil, err
	}
	page := resp.Crate.Homepage
	if page == "" {
		page = resp.Crate.Repository
	}
	if page == "" {
		page = fmt.Sprintf("https://crates.io/crates/%s", crate)
	}
	return &Result{Version: resp.Crate.MaxVersion, URL: page}, nil
}

// ---------- Go module ----------

// GoModuleSource go_module 源：通过 proxy.golang.org 查 @latest。
type GoModuleSource struct{}

func (s *GoModuleSource) Type() string { return "go_module" }

func (s *GoModuleSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Go Module",
		Icon:  "mdi-language-go",
		Fields: []FieldSpec{
			{Key: "module", Label: "Module 路径", Type: "string", Required: true, Placeholder: "github.com/xxx/yyy"},
		},
	}
}

func (s *GoModuleSource) Validate(cfg map[string]any) error {
	return required(cfg, "module")
}

type goproxyLatest struct {
	Version string `json:"Version"`
}

func (s *GoModuleSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	mod := strings.TrimSuffix(cfgString(cfg, "module"), "/")
	var l goproxyLatest
	u := fmt.Sprintf("https://proxy.golang.org/%s/@latest", mod)
	if err := fetchJSON(ctx, Options{}, u, nil, &l); err != nil {
		return nil, err
	}
	return &Result{
		Version: l.Version,
		URL:     fmt.Sprintf("https://pkg.go.dev/%s@%s", mod, l.Version),
	}, nil
}
