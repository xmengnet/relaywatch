package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebhookSend(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody webhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	n := &WebhookNotifier{}
	cfg := map[string]any{"url": srv.URL + "/hook", "secret": "s3cret"}
	msg := &Message{
		Title: "T", Body: "B",
		Monitor: MonitorInfo{Name: "mygo", SourceType: "github_release"},
		Change:  ChangeInfo{OldVersion: "0.1.0", NewVersion: "0.1.20"},
	}
	if err := n.Send(context.Background(), cfg, msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/hook" {
		t.Errorf("path = %s", gotPath)
	}
	if gotAuth != "Bearer s3cret" {
		t.Errorf("auth = %s", gotAuth)
	}
	if gotBody.Monitor.Name != "mygo" || gotBody.Change.NewVersion != "0.1.20" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestWebhookPermanentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()
	n := &WebhookNotifier{}
	err := n.Send(context.Background(), map[string]any{"url": srv.URL}, &Message{})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*PermanentError); !ok {
		t.Errorf("expected PermanentError, got %T: %v", err, err)
	}
}

func TestTelegramSend(t *testing.T) {
	var gotPath string
	var gotBody tgRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// 重定向 API 地址不可配置——通过替换 httpPost 验证请求构造
	orig := httpPost
	defer func() { httpPost = orig }()
	httpPost = func(ctx context.Context, url string, headers map[string]string, body []byte) (int, []byte, error) {
		gotPath = url
		if !strings.Contains(url, "/botTOK/sendMessage") {
			return 0, nil, http.ErrAbortHandler
		}
		_ = json.Unmarshal(body, &gotBody)
		return 200, []byte(`{"ok":true}`), nil
	}

	n := &TelegramNotifier{}
	err := n.Send(context.Background(), map[string]any{"bot_token": "TOK", "chat_id": "@chan"}, &Message{Body: "hi"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(gotPath, "/botTOK/sendMessage") {
		t.Errorf("url = %s", gotPath)
	}
	if gotBody.ChatID != "@chan" || gotBody.Text != "hi" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestRenderDefault(t *testing.T) {
	data := TemplateData{
		Monitor: MonitorInfo{Name: "mygo", SourceType: "github_release"},
		Change:  ChangeInfo{OldVersion: "0.1.0", NewVersion: "0.1.20", DetectedAt: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)},
		URL:     "https://github.com/x/y/releases/tag/v0.1.20",
	}
	title, body, err := Render("", "", data, true)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if title == "" || body == "" {
		t.Fatalf("empty output: %q / %q", title, body)
	}
	for _, want := range []string{"mygo", "0.1.0", "0.1.20", "github_release"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %q", want, body)
		}
	}
}

func TestRenderCustom(t *testing.T) {
	data := TemplateData{
		Monitor: MonitorInfo{Name: "pkg"},
		Change:  ChangeInfo{NewVersion: "2.0"},
	}
	custom := `{{ .Monitor.Name | upper }}: {{ .Change.NewVersion }}`
	_, body, err := Render("", custom, data, false)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if body != "PKG: 2.0" {
		t.Errorf("body = %q", body)
	}
}

func TestRenderInvalidFallsBack(t *testing.T) {
	data := TemplateData{Monitor: MonitorInfo{Name: "x"}, Change: ChangeInfo{NewVersion: "1"}}
	// 正文模板非法：body 报错返回 err，title 仍回退
	_, _, err := Render("", "{{ .Monitor.Name", data, false)
	if err == nil {
		t.Error("expected error for invalid template")
	}
}

func TestValidateRequired(t *testing.T) {
	if err := (&WebhookNotifier{}).Validate(map[string]any{}); err == nil {
		t.Error("expected error for missing url")
	}
	if err := (&WebhookNotifier{}).Validate(map[string]any{"url": "http://x"}); err != nil {
		t.Errorf("unexpected: %v", err)
	}
	if err := (&TelegramNotifier{}).Validate(map[string]any{"bot_token": "t"}); err == nil {
		t.Error("expected error for missing chat_id")
	}
}
