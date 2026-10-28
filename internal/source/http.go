package source

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"time"
)

// Options 源请求的公共配置（超时、UA），由 Checker 注入。
type Options struct {
	Timeout    time.Duration
	UserAgent  string
	HTTPClient *http.Client // 供测试注入，nil 则按 Timeout 构造
}

func (o Options) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	t := o.Timeout
	if t <= 0 {
		t = 20 * time.Second
	}
	return &http.Client{Timeout: t}
}

func (o Options) ua() string {
	if o.UserAgent != "" {
		return o.UserAgent
	}
	return "RelayWatch/0.1"
}

// fetchJSON GET 一个 JSON API 并解码；404 返回 *NotFoundError。
// 出站 URL 先经 RewriteURL 做镜像/代理改写（「网站管理」配置，无规则时原样）。
func fetchJSON(ctx context.Context, o Options, url string, headers map[string]string, out any) error {
	url = RewriteURL(url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", o.ua())
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := o.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return &NotFoundError{Msg: fmt.Sprintf("HTTP 404: %s", url)}
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("rate limited (HTTP 429)")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// fetchXML GET 一个 XML 端点并解码；404 返回 *NotFoundError。
func fetchXML(ctx context.Context, o Options, url string, headers map[string]string, out any) error {
	url = RewriteURL(url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", o.ua())
	req.Header.Set("Accept", "application/xml, text/xml")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := o.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return &NotFoundError{Msg: fmt.Sprintf("HTTP 404: %s", url)}
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("rate limited (HTTP 429)")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	return xml.NewDecoder(resp.Body).Decode(out)
}
