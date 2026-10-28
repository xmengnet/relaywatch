package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// AURSource aur 源：AUR 包当前版本。
type AURSource struct{}

func (s *AURSource) Type() string { return "aur" }

func (s *AURSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "AUR",
		Icon:  "mdi-arch",
		Fields: []FieldSpec{
			{Key: "pkgname", Label: "包名", Type: "string", Required: true, Placeholder: "yay"},
		},
	}
}

func (s *AURSource) Validate(cfg map[string]any) error {
	return required(cfg, "pkgname")
}

// AUR RPC v5 info 响应
type aurResponse struct {
	Version     int          `json:"version"` // RPC 协议版本
	Type        string       `json:"type"`
	ResultCount int          `json:"resultcount"`
	Results     []aurPackage `json:"results"`
}

type aurPackage struct {
	Name    string `json:"Name"`
	Version string `json:"Version"`
	URL     string `json:"URL"`
}

func (s *AURSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	pkg := cfgString(cfg, "pkgname")
	u := "https://aur.archlinux.org/rpc/v5/info?" + url.Values{"arg[]": []string{pkg}}.Encode()
	var resp aurResponse
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}
	if resp.ResultCount == 0 {
		return nil, &NotFoundError{Msg: "AUR 上不存在包 " + pkg}
	}
	p := resp.Results[0]
	return &Result{Version: p.Version, URL: "https://aur.archlinux.org/packages/" + p.Name}, nil
}

// ArchOfficialSource arch_official 源：Arch 官方仓库版本。
type ArchOfficialSource struct{}

func (s *ArchOfficialSource) Type() string { return "arch_official" }

func (s *ArchOfficialSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Arch 官方仓库",
		Icon:  "mdi-linux",
		Fields: []FieldSpec{
			{Key: "pkgname", Label: "包名", Type: "string", Required: true, Placeholder: "linux"},
			{Key: "repo", Label: "仓库", Type: "select", Options: []string{"", "core", "extra", "multilib", "core-testing", "extra-testing", "multilib-testing"}, Help: "留空查全部官方仓库"},
		},
	}
}

func (s *ArchOfficialSource) Validate(cfg map[string]any) error {
	return required(cfg, "pkgname")
}

type archSearchResponse struct {
	Version int             `json:"version"`
	Limit   int             `json:"limit"`
	Valid   bool            `json:"valid"`
	Results []archPkgResult `json:"results"`
}

type archPkgResult struct {
	PkgName string `json:"pkgname"`
	Repo    string `json:"repo"`
	Version string `json:"pkgver"`
	// Arch 官方 API 的 pkgver 不含 pkgrel
}

func (s *ArchOfficialSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	pkg := cfgString(cfg, "pkgname")
	repo := cfgString(cfg, "repo")
	u := "https://archlinux.org/packages/search/json/?q=" + url.QueryEscape(pkg)
	if repo != "" {
		u += "&repo=" + url.QueryEscape(repo)
	}
	var resp archSearchResponse
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}
	if !resp.Valid || len(resp.Results) == 0 {
		return nil, &NotFoundError{Msg: "Arch 官方仓库查无 " + pkg}
	}
	// 同一包多架构会返回多行，取第一行的 pkgver（同一 repo 内版本一致）
	p := resp.Results[0]
	label := p.Repo
	if repo == "" {
		for _, r := range resp.Results {
			if r.Repo != p.Repo {
				// 多仓库都有时标注，如 core/extra
			}
		}
	}
	return &Result{
		Version: p.Version,
		URL:     archPkgURL(repo, p.Repo, p.PkgName),
		Extra:   map[string]any{"repo": label},
	}, nil
}

func archPkgURL(repoFilter, actualRepo, pkgName string) string {
	if repoFilter == "" {
		repoFilter = actualRepo
	}
	return fmt.Sprintf("https://archlinux.org/packages/%s/%s/", strings.ToLower(repoFilter), pkgName)
}
