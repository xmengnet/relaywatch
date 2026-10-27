package source

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
)

// ---------- Repology ----------

// RepologySource repology 源：跨发行版软件包状态查询。
type RepologySource struct{}

func (s *RepologySource) Type() string { return "repology" }

func (s *RepologySource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Repology",
		Icon:  "mdi-package-variant-closed",
		Fields: []FieldSpec{
			{Key: "project", Label: "项目名", Type: "string", Required: true, Placeholder: "ripgrep", Help: "Repology 上的项目名称"},
			{Key: "repo", Label: "目标仓库", Type: "string", Required: true, Placeholder: "arch", Help: "例如 arch, aur, debian_unstable 等"},
			{Key: "subrepo", Label: "子仓库", Type: "string", Help: "可选，例如 extra"},
		},
	}
}

func (s *RepologySource) Validate(cfg map[string]any) error {
	return required(cfg, "project", "repo")
}

type repologyItem struct {
	Repo    string `json:"repo"`
	Subrepo string `json:"subrepo"`
	Version string `json:"version"`
	Status  string `json:"status"`
}

func (s *RepologySource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	project := cfgString(cfg, "project")
	repo := cfgString(cfg, "repo")
	subrepo := cfgString(cfg, "subrepo")
	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://repology.org"
	}

	u := fmt.Sprintf("%s/api/v1/project/%s", base, url.PathEscape(project))
	var items []repologyItem
	if err := fetchJSON(ctx, Options{}, u, nil, &items); err != nil {
		return nil, err
	}

	var best string
	for _, item := range items {
		if item.Repo != repo {
			continue
		}
		if subrepo != "" && item.Subrepo != subrepo {
			continue
		}
		if best == "" || greaterVersion(item.Version, best) {
			best = item.Version
		}
	}

	if best == "" {
		return nil, &NotFoundError{Msg: fmt.Sprintf("项目 %s 在仓库 %s 中未找到匹配的包", project, repo)}
	}

	return &Result{
		Version: best,
		URL:     fmt.Sprintf("https://repology.org/project/%s/packages", project),
	}, nil
}

// ---------- Anitya (release-monitoring.org) ----------

// AnityaSource anitya 源：Fedora 维护的 release-monitoring.org 开源上游追踪平台。
type AnityaSource struct{}

func (s *AnityaSource) Type() string { return "anitya" }

func (s *AnityaSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Anitya (Release Monitoring)",
		Icon:  "mdi-radar",
		Fields: []FieldSpec{
			{Key: "project", Label: "项目 ID 或标识", Type: "string", Required: true, Placeholder: "13815", Help: "release-monitoring.org 上的项目 ID 或名称"},
		},
	}
}

func (s *AnityaSource) Validate(cfg map[string]any) error {
	return required(cfg, "project")
}

type anityaResponse struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (s *AnityaSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	proj := cfgString(cfg, "project")
	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://release-monitoring.org"
	}
	u := fmt.Sprintf("%s/api/project/%s", base, url.PathEscape(proj))
	var resp anityaResponse
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Version == "" {
		return nil, &NotFoundError{Msg: fmt.Sprintf("Anitya 项目 %s 暂无版本记录", proj)}
	}
	return &Result{
		Version: resp.Version,
		URL:     fmt.Sprintf("https://release-monitoring.org/project/%d/", resp.ID),
	}, nil
}

// ---------- RubyGems ----------

// RubyGemsSource rubygems 源。
type RubyGemsSource struct{}

func (s *RubyGemsSource) Type() string { return "rubygems" }

func (s *RubyGemsSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "RubyGems",
		Icon:  "mdi-language-ruby",
		Fields: []FieldSpec{
			{Key: "gem", Label: "Gem 包名", Type: "string", Required: true, Placeholder: "rails"},
			{Key: "include_prerelease", Label: "包含预发布版本", Type: "bool"},
		},
	}
}

func (s *RubyGemsSource) Validate(cfg map[string]any) error {
	return required(cfg, "gem")
}

type rubyVersionItem struct {
	Number     string `json:"number"`
	Prerelease bool   `json:"prerelease"`
}

func (s *RubyGemsSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	gem := cfgString(cfg, "gem")
	includePre := cfgBool(cfg, "include_prerelease")
	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://rubygems.org"
	}

	u := fmt.Sprintf("%s/api/v1/versions/%s.json", base, url.PathEscape(gem))
	var list []rubyVersionItem
	if err := fetchJSON(ctx, Options{}, u, nil, &list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, &NotFoundError{Msg: fmt.Sprintf("RubyGems 上不存在 %s", gem)}
	}

	var best string
	for _, item := range list {
		if !includePre && item.Prerelease {
			continue
		}
		if best == "" || greaterVersion(item.Number, best) {
			best = item.Number
		}
	}

	if best == "" {
		return nil, fmt.Errorf("没有符合条件的稳定版本")
	}

	return &Result{
		Version: best,
		URL:     fmt.Sprintf("https://rubygems.org/gems/%s/versions/%s", gem, best),
	}, nil
}

// ---------- Maven ----------

// MavenSource maven 源：Maven Central 或私有 Maven 仓库。
type MavenSource struct{}

func (s *MavenSource) Type() string { return "maven" }

func (s *MavenSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Maven",
		Icon:  "mdi-language-java",
		Fields: []FieldSpec{
			{Key: "group", Label: "Group ID", Type: "string", Required: true, Placeholder: "org.apache.commons"},
			{Key: "artifact", Label: "Artifact ID", Type: "string", Required: true, Placeholder: "commons-lang3"},
			{Key: "repo", Label: "Maven 仓库地址", Type: "string", Placeholder: "https://repo1.maven.org/maven2", Help: "默认使用 Maven Central"},
		},
	}
}

func (s *MavenSource) Validate(cfg map[string]any) error {
	return required(cfg, "group", "artifact")
}

type mavenMetadataXML struct {
	XMLName    xml.Name `xml:"metadata"`
	GroupID    string   `xml:"groupId"`
	ArtifactID string   `xml:"artifactId"`
	Versioning struct {
		Latest   string   `xml:"latest"`
		Release  string   `xml:"release"`
		Versions []string `xml:"versions>version"`
	} `xml:"versioning"`
}

func (s *MavenSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	group := cfgString(cfg, "group")
	artifact := cfgString(cfg, "artifact")
	repo := strings.TrimRight(cfgString(cfg, "repo"), "/")
	if repo == "" {
		repo = "https://repo1.maven.org/maven2"
	}

	groupPath := strings.ReplaceAll(group, ".", "/")
	u := fmt.Sprintf("%s/%s/%s/maven-metadata.xml", repo, groupPath, url.PathEscape(artifact))

	var meta mavenMetadataXML
	if err := fetchXML(ctx, Options{}, u, nil, &meta); err != nil {
		return nil, err
	}

	// 优先读取 <release>，次选 <latest>，最后遍历 <versions>
	ver := meta.Versioning.Release
	if ver == "" {
		ver = meta.Versioning.Latest
	}
	if ver == "" && len(meta.Versioning.Versions) > 0 {
		for _, v := range meta.Versioning.Versions {
			if ver == "" || greaterVersion(v, ver) {
				ver = v
			}
		}
	}
	if ver == "" {
		return nil, fmt.Errorf("未能从 maven-metadata.xml 解析到版本信息")
	}

	return &Result{
		Version: ver,
		URL:     fmt.Sprintf("%s/%s/%s/", repo, groupPath, artifact),
	}, nil
}

// ---------- Packagist ----------

// PackagistSource packagist 源：PHP Composer 包。
type PackagistSource struct{}

func (s *PackagistSource) Type() string { return "packagist" }

func (s *PackagistSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Packagist (PHP)",
		Icon:  "mdi-language-php",
		Fields: []FieldSpec{
			{Key: "package", Label: "包名", Type: "string", Required: true, Placeholder: "monolog/monolog", Help: "格式: vendor/name"},
			{Key: "include_prerelease", Label: "包含预发布版本", Type: "bool"},
		},
	}
}

func (s *PackagistSource) Validate(cfg map[string]any) error {
	if err := required(cfg, "package"); err != nil {
		return err
	}
	pkg := cfgString(cfg, "package")
	if strings.Count(pkg, "/") != 1 {
		return fmt.Errorf("包名需为 vendor/name 格式")
	}
	return nil
}

type packagistPkgResp struct {
	Package struct {
		Name     string                      `json:"name"`
		Versions map[string]packagistVersion `json:"versions"`
	} `json:"package"`
}

type packagistVersion struct {
	Version string `json:"version"`
	Time    string `json:"time"`
}

func (s *PackagistSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	pkg := cfgString(cfg, "package")
	includePre := cfgBool(cfg, "include_prerelease")
	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://packagist.org"
	}

	u := fmt.Sprintf("%s/packages/%s.json", base, pkg)
	var resp packagistPkgResp
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.Package.Versions) == 0 {
		return nil, &NotFoundError{Msg: fmt.Sprintf("Packagist 上不存在包 %s", pkg)}
	}

	var best string
	for verName, v := range resp.Package.Versions {
		// 排除 dev- 开头分支
		if strings.HasPrefix(verName, "dev-") || strings.HasSuffix(verName, "-dev") {
			continue
		}
		if !includePre && isPrerelease(verName) {
			continue
		}
		ver := v.Version
		if ver == "" {
			ver = verName
		}
		if best == "" || greaterVersion(ver, best) {
			best = ver
		}
	}

	if best == "" {
		return nil, fmt.Errorf("没有符合条件的稳定版本")
	}

	return &Result{
		Version: best,
		URL:     fmt.Sprintf("https://packagist.org/packages/%s#%s", pkg, best),
	}, nil
}

func isPrerelease(v string) bool {
	lower := strings.ToLower(v)
	for _, tag := range []string{"alpha", "beta", "rc", "preview", "b", "a"} {
		if strings.Contains(lower, tag) {
			return true
		}
	}
	return false
}

// ---------- CPAN ----------

// CPANSource cpan 源：Perl MetaCPAN。
type CPANSource struct{}

func (s *CPANSource) Type() string { return "cpan" }

func (s *CPANSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "CPAN (Perl)",
		Icon:  "mdi-tune-vertical",
		Fields: []FieldSpec{
			{Key: "module", Label: "模块或发行版名称", Type: "string", Required: true, Placeholder: "Mojolicious"},
		},
	}
}

func (s *CPANSource) Validate(cfg map[string]any) error {
	return required(cfg, "module")
}

type cpanReleaseResponse struct {
	Name    string `json:"name"`
	Author  string `json:"author"`
	Version any    `json:"version"` // 有可能是数字或字符串
}

func (s *CPANSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	mod := cfgString(cfg, "module")
	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://fastapi.metacpan.org"
	}

	u := fmt.Sprintf("%s/release/%s", base, url.PathEscape(mod))
	var resp cpanReleaseResponse
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}

	ver := fmt.Sprintf("%v", resp.Version)
	if ver == "" || ver == "<nil>" {
		return nil, &NotFoundError{Msg: fmt.Sprintf("CPAN 模块 %s 未获取到有效版本", mod)}
	}

	return &Result{
		Version: ver,
		URL:     fmt.Sprintf("https://metacpan.org/release/%s/%s", resp.Author, resp.Name),
	}, nil
}

// ---------- CRAN ----------

// CRANSource cran 源：R 语言 CRAN 仓库。
type CRANSource struct{}

func (s *CRANSource) Type() string { return "cran" }

func (s *CRANSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "CRAN (R)",
		Icon:  "mdi-language-r",
		Fields: []FieldSpec{
			{Key: "package", Label: "包名", Type: "string", Required: true, Placeholder: "ggplot2"},
		},
	}
}

func (s *CRANSource) Validate(cfg map[string]any) error {
	return required(cfg, "package")
}

type cranResponse struct {
	Package string `json:"Package"`
	Version string `json:"Version"`
}

func (s *CRANSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	pkg := cfgString(cfg, "package")
	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://crandb.r-pkg.org"
	}

	u := fmt.Sprintf("%s/%s", base, url.PathEscape(pkg))
	var resp cranResponse
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}

	if resp.Version == "" {
		return nil, &NotFoundError{Msg: fmt.Sprintf("CRAN 包 %s 未获取到版本", pkg)}
	}

	return &Result{
		Version: resp.Version,
		URL:     fmt.Sprintf("https://cran.r-project.org/package=%s", pkg),
	}, nil
}

// ---------- Hackage ----------

// HackageSource hackage 源：Haskell Hackage 官方仓库。
type HackageSource struct{}

func (s *HackageSource) Type() string { return "hackage" }

func (s *HackageSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Hackage (Haskell)",
		Icon:  "mdi-language-haskell",
		Fields: []FieldSpec{
			{Key: "package", Label: "包名", Type: "string", Required: true, Placeholder: "pandoc"},
		},
	}
}

func (s *HackageSource) Validate(cfg map[string]any) error {
	return required(cfg, "package")
}

type hackageResponse struct {
	NormalVersion []string `json:"normal-version"`
}

func (s *HackageSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	pkg := cfgString(cfg, "package")
	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://hackage.haskell.org"
	}

	u := fmt.Sprintf("%s/package/%s/preferred.json", base, url.PathEscape(pkg))
	var resp hackageResponse
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}

	if len(resp.NormalVersion) == 0 {
		return nil, &NotFoundError{Msg: fmt.Sprintf("Hackage 包 %s 没有 preferred 版本", pkg)}
	}

	ver := resp.NormalVersion[0]
	return &Result{
		Version: ver,
		URL:     fmt.Sprintf("https://hackage.haskell.org/package/%s-%s", pkg, ver),
	}, nil
}

