package notifier

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const httpTimeout = 20 * time.Second

func defaultHTTPPost(ctx context.Context, url string, headers map[string]string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "RelayWatch/0.1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

// checkStatus 把 HTTP 状态码转成错误语义：2xx 成功，4xx 永久，5xx 可重试。
func checkStatus(status int, body []byte, where string) error {
	if status >= 200 && status < 300 {
		return nil
	}
	msg := fmt.Sprintf("%s: HTTP %d: %s", where, status, truncate(string(body), 200))
	if status >= 400 && status < 500 {
		return &PermanentError{Err: fmt.Errorf("%s", msg)}
	}
	return fmt.Errorf("%s", msg)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// cfgString/cfgBool 与 source 包一致的配置读取助手。
func cfgString(cfg map[string]any, key string) string {
	v, _ := cfg[key].(string)
	return strings.TrimSpace(v)
}

func cfgBool(cfg map[string]any, key string) bool {
	switch v := cfg[key].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1" || v == "yes"
	}
	return false
}

func required(cfg map[string]any, keys ...string) error {
	for _, k := range keys {
		if cfgString(cfg, k) == "" {
			return fmt.Errorf("缺少必填参数: %s", k)
		}
	}
	return nil
}
