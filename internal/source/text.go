package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// fetchText GET 页面并返回文本正文；404 转 NotFoundError。
func fetchText(ctx context.Context, o Options, url string, headers map[string]string) (string, error) {
	url = RewriteURL(url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", o.ua())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := o.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", &NotFoundError{Msg: fmt.Sprintf("HTTP 404: %s", url)}
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", fmt.Errorf("rate limited (HTTP 429)")
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8MB 上限
	if err != nil {
		return "", err
	}
	return string(data), nil
}

var _ = strings.TrimSpace
