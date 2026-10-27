package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---------- OpenVSX ----------

// OpenVSXSource openvsx 源：开源 VS Code 插件市场。
type OpenVSXSource struct{}

func (s *OpenVSXSource) Type() string { return "openvsx" }

func (s *OpenVSXSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Open VSX 插件市场",
		Icon:  "mdi-puzzle",
		Fields: []FieldSpec{
			{Key: "extension", Label: "插件标识", Type: "string", Required: true, Placeholder: "rust-lang.rust-analyzer", Help: "格式: publisher.name"},
		},
	}
}

func (s *OpenVSXSource) Validate(cfg map[string]any) error {
	ext := cfgString(cfg, "extension")
	if ext == "" {
		return fmt.Errorf("缺少必填参数: extension")
	}
	if !strings.Contains(ext, ".") {
		return fmt.Errorf("extension 需为 publisher.name 格式")
	}
	return nil
}

type openVSXResponse struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

func (s *OpenVSXSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	ext := cfgString(cfg, "extension")
	parts := strings.SplitN(ext, ".", 2)
	publisher, name := parts[0], parts[1]

	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://open-vsx.org"
	}

	u := fmt.Sprintf("%s/api/%s/%s", base, url.PathEscape(publisher), url.PathEscape(name))
	var resp openVSXResponse
	if err := fetchJSON(ctx, Options{}, u, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Version == "" {
		return nil, &NotFoundError{Msg: fmt.Sprintf("Open VSX 插件 %s 暂无版本信息", ext)}
	}

	pageURL := resp.URL
	if pageURL == "" {
		pageURL = fmt.Sprintf("https://open-vsx.org/extension/%s/%s", publisher, name)
	}

	return &Result{
		Version: resp.Version,
		URL:     pageURL,
	}, nil
}

// ---------- VS Marketplace ----------

// VSMarketplaceSource vsmarketplace 源：官方 Visual Studio Marketplace。
type VSMarketplaceSource struct{}

func (s *VSMarketplaceSource) Type() string { return "vsmarketplace" }

func (s *VSMarketplaceSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "VS Code 插件市场",
		Icon:  "mdi-microsoft-visual-studio-code",
		Fields: []FieldSpec{
			{Key: "extension", Label: "插件标识", Type: "string", Required: true, Placeholder: "golang.go", Help: "格式: publisher.name"},
		},
	}
}

func (s *VSMarketplaceSource) Validate(cfg map[string]any) error {
	ext := cfgString(cfg, "extension")
	if ext == "" {
		return fmt.Errorf("缺少必填参数: extension")
	}
	if !strings.Contains(ext, ".") {
		return fmt.Errorf("extension 需为 publisher.name 格式")
	}
	return nil
}

type vsMarketplaceReq struct {
	Filters []vsFilter `json:"filters"`
	Flags   int        `json:"flags"`
}

type vsFilter struct {
	Criteria   []vsCriterion `json:"criteria"`
	PageNumber int           `json:"pageNumber"`
	PageSize   int           `json:"pageSize"`
}

type vsCriterion struct {
	FilterType int    `json:"filterType"`
	Value      string `json:"value"`
}

type vsMarketplaceResp struct {
	Results []struct {
		Extensions []struct {
			Versions []struct {
				Version string `json:"version"`
			} `json:"versions"`
		} `json:"extensions"`
	} `json:"results"`
}

func (s *VSMarketplaceSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	ext := cfgString(cfg, "extension")
	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://marketplace.visualstudio.com"
	}

	u := fmt.Sprintf("%s/_apis/public/gallery/extensionquery", base)
	reqBody := vsMarketplaceReq{
		Filters: []vsFilter{
			{
				Criteria: []vsCriterion{
					{FilterType: 8, Value: "Microsoft.VisualStudio.Code"},
					{FilterType: 7, Value: ext},
					{FilterType: 12, Value: "4096"},
				},
				PageNumber: 1,
				PageSize:   1,
			},
		},
		Flags: 946,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RelayWatch/0.1 (VSMarketplace Client)")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json;api-version=6.1-preview.1")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, &NotFoundError{Msg: fmt.Sprintf("VS Marketplace 插件 %s 不存在", ext)}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("VS Marketplace 返回 HTTP %d", resp.StatusCode)
	}

	var data vsMarketplaceResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	if len(data.Results) == 0 || len(data.Results[0].Extensions) == 0 || len(data.Results[0].Extensions[0].Versions) == 0 {
		return nil, &NotFoundError{Msg: fmt.Sprintf("插件 %s 未查询到版本信息", ext)}
	}

	ver := data.Results[0].Extensions[0].Versions[0].Version
	return &Result{
		Version: ver,
		URL:     fmt.Sprintf("https://marketplace.visualstudio.com/items?itemName=%s", ext),
	}, nil
}

// ---------- Snapcraft ----------

// SnapcraftSource snapcraft 源：Ubuntu Snap 商店。
type SnapcraftSource struct{}

func (s *SnapcraftSource) Type() string { return "snapcraft" }

func (s *SnapcraftSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "Snapcraft 商店",
		Icon:  "mdi-package",
		Fields: []FieldSpec{
			{Key: "snap", Label: "Snap 名称", Type: "string", Required: true, Placeholder: "canonical-livepatch"},
			{Key: "channel", Label: "发布频道", Type: "string", Default: "stable", Help: "例如 stable, candidate, beta, edge；默认 stable"},
		},
	}
}

func (s *SnapcraftSource) Validate(cfg map[string]any) error {
	return required(cfg, "snap")
}

type snapcraftResponse struct {
	ChannelMap []struct {
		Channel struct {
			Name string `json:"name"`
		} `json:"channel"`
		Version string `json:"version"`
	} `json:"channel-map"`
}

func (s *SnapcraftSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	snap := cfgString(cfg, "snap")
	targetChannel := cfgString(cfg, "channel")
	if targetChannel == "" {
		targetChannel = "stable"
	}

	base := strings.TrimRight(cfgString(cfg, "api_base"), "/")
	if base == "" {
		base = "https://api.snapcraft.io"
	}

	u := fmt.Sprintf("%s/v2/snaps/info/%s", base, url.PathEscape(snap))
	headers := map[string]string{
		"Snap-Device-Series": "16",
	}

	var resp snapcraftResponse
	if err := fetchJSON(ctx, Options{}, u, headers, &resp); err != nil {
		return nil, err
	}

	var matchVersion string
	for _, item := range resp.ChannelMap {
		if item.Channel.Name == targetChannel {
			matchVersion = item.Version
			break
		}
	}

	if matchVersion == "" {
		return nil, &NotFoundError{Msg: fmt.Sprintf("Snap %s 在频道 %s 暂无可用版本", snap, targetChannel)}
	}

	return &Result{
		Version: matchVersion,
		URL:     fmt.Sprintf("https://snapcraft.io/%s", snap),
	}, nil
}
