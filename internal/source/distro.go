package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// ---------- DebianPkg ----------

// DebianPkgSource debianpkg 源：查询 sources.debian.org API。
type DebianPkgSource struct{}

func (s *DebianPkgSource) Type() string { return "debianpkg" }

func (s *DebianPkgSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Debian 软件包",
		Icon:  "mdi-debian",
		Fields: []FieldSpec{
			{Key: "pkgname", Label: "源码包名", Type: "string", Required: true, Placeholder: "ripgrep"},
			{Key: "suite", Label: "Suite / 发行版", Type: "string", Default: "sid", Help: "例如 sid, bookworm, trixie，默认 sid"},
			{Key: "strip_release", Label: "剥离 Release 后缀", Type: "bool", Help: "剥离形如 -1 的 Debian 修订号"},
		},
	}
}

func (s *DebianPkgSource) Validate(cfg map[string]any) error {
	return required(cfg, "pkgname")
}

type debianResponse struct {
	Package  string `json:"package"`
	Versions []struct {
		Version string `json:"version"`
		Suite   string `json:"suite"`
	} `json:"versions"`
}

func (s *DebianPkgSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	pkg := cfgString(cfg, "pkgname")
	suite := cfgString(cfg, "suite")
	if suite == "" {
		suite = "sid"
	}
	stripRelease := cfgBool(cfg, "strip_release")

	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://sources.debian.org"
	}

	u := fmt.Sprintf("%s/api/src/%s/?suite=%s", base, url.PathEscape(pkg), url.QueryEscape(suite))
	var resp debianResponse
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.Versions) == 0 {
		return nil, &NotFoundError{Msg: fmt.Sprintf("Debian 在 %s 中未找到包 %s", suite, pkg)}
	}

	rawVersion := resp.Versions[0].Version
	finalVersion := rawVersion
	if stripRelease && strings.Contains(finalVersion, "-") {
		// 剥离最后一段 release (如 1.2.3-1 -> 1.2.3)
		finalVersion = finalVersion[:strings.LastIndex(finalVersion, "-")]
	}

	return &Result{
		Version: finalVersion,
		URL:     fmt.Sprintf("https://sources.debian.org/src/%s/%s/", resp.Package, rawVersion),
	}, nil
}

// ---------- UbuntuPkg ----------

// UbuntuPkgSource ubuntupkg 源：查询 Canonical Launchpad API。
type UbuntuPkgSource struct{}

func (s *UbuntuPkgSource) Type() string { return "ubuntupkg" }

func (s *UbuntuPkgSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Ubuntu 软件包",
		Icon:  "mdi-ubuntu",
		Fields: []FieldSpec{
			{Key: "pkgname", Label: "源码包名", Type: "string", Required: true, Placeholder: "curl"},
			{Key: "suite", Label: "Suite / 发行系列", Type: "string", Help: "例如 noble, jammy；留空查默认主要系列"},
			{Key: "strip_release", Label: "剥离 Release 后缀", Type: "bool", Help: "剥离 Ubuntu 修订号"},
		},
	}
}

func (s *UbuntuPkgSource) Validate(cfg map[string]any) error {
	return required(cfg, "pkgname")
}

type launchpadResponse struct {
	Entries []struct {
		Status               string `json:"status"`
		SourcePackageVersion string `json:"source_package_version"`
		DistroSeriesLink     string `json:"distro_series_link"`
	} `json:"entries"`
}

func (s *UbuntuPkgSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	pkg := cfgString(cfg, "pkgname")
	suite := cfgString(cfg, "suite")
	stripRelease := cfgBool(cfg, "strip_release")

	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://api.launchpad.net"
	}

	u := fmt.Sprintf("%s/1.0/ubuntu/+archive/primary?ws.op=getPublishedSources&source_name=%s&exact_match=true", base, url.QueryEscape(pkg))
	var resp launchpadResponse
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}

	var matchVersion string
	var matchSuite string
	for _, entry := range resp.Entries {
		if entry.Status != "Published" {
			continue
		}
		if suite != "" && !strings.HasSuffix(entry.DistroSeriesLink, "/"+suite) {
			continue
		}
		matchVersion = entry.SourcePackageVersion
		// 解析 series
		parts := strings.Split(strings.TrimRight(entry.DistroSeriesLink, "/"), "/")
		if len(parts) > 0 {
			matchSuite = parts[len(parts)-1]
		}
		break
	}

	if matchVersion == "" {
		return nil, &NotFoundError{Msg: fmt.Sprintf("Ubuntu 未找到包 %s 的已发布源码", pkg)}
	}

	finalVersion := matchVersion
	if stripRelease && strings.Contains(finalVersion, "-") {
		finalVersion = finalVersion[:strings.LastIndex(finalVersion, "-")]
	}

	pageURL := fmt.Sprintf("https://packages.ubuntu.com/%s", pkg)
	if matchSuite != "" {
		pageURL = fmt.Sprintf("https://packages.ubuntu.com/%s/%s", matchSuite, pkg)
	}

	return &Result{
		Version: finalVersion,
		URL:     pageURL,
	}, nil
}
