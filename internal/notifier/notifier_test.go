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

func compareTestData(event, state, follower, ref string) TemplateData {
	return TemplateData{
		Monitor: MonitorInfo{Name: "kun", SourceType: "aur", Tags: []string{"arch"}},
		Change:  ChangeInfo{DetectedAt: time.Date(2026, 10, 28, 13, 30, 0, 0, time.UTC)},
		Compare: CompareInfo{
			RefMonitorName: "KunAgent/Kun", RefVersion: ref,
			FollowerVersion: follower, State: state, Event: event,
		},
		URL: "https://github.com/KunAgent/Kun/releases/tag/v0.4.3",
	}
}

func TestRenderCompareDefaultEnteredLagging(t *testing.T) {
	title, body, err := RenderCompare("", "", compareTestData("entered_lagging", "lagging", "0.2.20-2", "v0.4.3"), true)
	if err != nil {
		t.Fatalf("RenderCompare: %v", err)
	}
	if !strings.Contains(title, "落后于上游") {
		t.Errorf("title = %q", title)
	}
	for _, want := range []string{"kun", "aur", "0.2.20-2", "KunAgent/Kun", "v0.4.3", "差距", "落后", "上游链接"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

func TestRenderCompareDefaultGapGrew(t *testing.T) {
	_, body, err := RenderCompare("", "", compareTestData("gap_grew", "lagging", "0.2.20-2", "v0.4.3"), true)
	if err != nil {
		t.Fatalf("RenderCompare: %v", err)
	}
	if !strings.Contains(body, "落后幅度扩大") || !strings.Contains(body, "上游又发布了新版本") {
		t.Errorf("gap_grew body should mention widening gap:\n%s", body)
	}
}

func TestRenderCompareDefaultCaughtUp(t *testing.T) {
	title, body, err := RenderCompare("", "", compareTestData("caught_up", "in_sync", "v0.4.3", "v0.4.3"), true)
	if err != nil {
		t.Fatalf("RenderCompare: %v", err)
	}
	if !strings.Contains(title, "已追平上游") {
		t.Errorf("title = %q", title)
	}
	for _, want := range []string{"已追平上游", "两侧版本一致", "已同步"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

func TestRenderDefaultFirstBaseline(t *testing.T) {
	data := TemplateData{
		Monitor: MonitorInfo{Name: "fresh", SourceType: "manual"},
		Change:  ChangeInfo{NewVersion: "1.0.0", DetectedAt: time.Date(2026, 10, 28, 13, 30, 0, 0, time.UTC)},
	}
	_, body, err := Render("", "", data, true)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(body, "新监控基线") {
		t.Errorf("first baseline body should be marked:\n%s", body)
	}
}

func TestRenderDefaultJoinTags(t *testing.T) {
	data := TemplateData{
		Monitor: MonitorInfo{Name: "x", SourceType: "aur", Tags: []string{"a", "b"}},
		Change:  ChangeInfo{NewVersion: "1"},
	}
	_, body, err := Render("", "", data, true)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(body, "标签：a, b") {
		t.Errorf("tags should render comma-separated:\n%s", body)
	}
}

// 飞书 post 富文本：标题行加粗、正文逐行成元素、**bold** 片段转 bold。
func TestFeishuRichtext(t *testing.T) {
	p := feishuRichtext("RelayWatch：kun 落后于上游 KunAgent/Kun", "⚠️ kun 落后于上游\n本包：**kun**（aur）0.2.20-2\n")
	if p.MsgType != "post" {
		t.Fatalf("msg_type = %q, want post", p.MsgType)
	}
	rows := p.Content.Post.ZhCN
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4（标题+2 行+末尾空行）", len(rows))
	}
	if !rows[0][0].Bold || rows[0][0].Text != "RelayWatch：kun 落后于上游 KunAgent/Kun" {
		t.Errorf("title row = %+v", rows[0])
	}
	// 第二行纯文本；第三行含粗体片段：["本包：", "kun"(bold), "（aur）0.2.20-2"]
	third := rows[2]
	if len(third) != 3 {
		t.Fatalf("third row elems = %d, want 3: %+v", len(third), third)
	}
	if third[0].Text != "本包：" || !third[1].Bold || third[1].Text != "kun" || third[2].Text != "（aur）0.2.20-2" {
		t.Errorf("third row = %+v", third)
	}
	// 空行补占位元素（第 3 行正文 + 第 4 行末尾空行）
	if len(rows[3]) != 1 || rows[3][0].Text != " " {
		t.Errorf("empty line row = %+v", rows[3])
	}
}

// 未配对的 ** 按普通文本处理。
func TestFeishuRichtextUnpairedBold(t *testing.T) {
	p := feishuRichtext("t", "a ** b")
	rows := p.Content.Post.ZhCN
	if len(rows) != 2 || len(rows[1]) != 1 || rows[1][0].Text != "a ** b" || rows[1][0].Bold {
		t.Fatalf("unpaired bold row = %+v", rows[1])
	}
}

// 企微：Markdown 消息走 markdown msgtype，text 兜底。
func TestWecomMarkdownPayload(t *testing.T) {
	var gotBody map[string]any
	orig := httpPost
	defer func() { httpPost = orig }()
	httpPost = func(ctx context.Context, url string, headers map[string]string, body []byte) (int, []byte, error) {
		_ = json.Unmarshal(body, &gotBody)
		return 200, []byte(`{"errcode":0}`), nil
	}
	n := &WecomNotifier{}
	if err := n.Send(context.Background(), map[string]any{"webhook_url": "http://x"}, &Message{Title: "标题", Body: "正文", Markdown: true}); err != nil {
		t.Fatal(err)
	}
	if gotBody["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v, want markdown", gotBody["msgtype"])
	}
	md := gotBody["markdown"].(map[string]any)
	if !strings.HasPrefix(md["content"].(string), "**标题**") {
		t.Errorf("markdown content = %v", md["content"])
	}

	gotBody = nil
	if err := n.Send(context.Background(), map[string]any{"webhook_url": "http://x"}, &Message{Title: "标题", Body: "正文"}); err != nil {
		t.Fatal(err)
	}
	if gotBody["msgtype"] != "text" {
		t.Fatalf("msgtype = %v, want text", gotBody["msgtype"])
	}
}

// 钉钉：Markdown 消息走 markdown msgtype 且带 title，text 兜底。
func TestDingTalkMarkdownPayload(t *testing.T) {
	var gotBody map[string]any
	orig := httpPost
	defer func() { httpPost = orig }()
	httpPost = func(ctx context.Context, url string, headers map[string]string, body []byte) (int, []byte, error) {
		_ = json.Unmarshal(body, &gotBody)
		return 200, []byte(`{"errcode":0}`), nil
	}
	n := &DingTalkNotifier{}
	if err := n.Send(context.Background(), map[string]any{"webhook_url": "http://x"}, &Message{Title: "标题", Body: "正文", Markdown: true}); err != nil {
		t.Fatal(err)
	}
	if gotBody["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v, want markdown", gotBody["msgtype"])
	}
	md := gotBody["markdown"].(map[string]any)
	if md["title"] != "标题" || !strings.HasPrefix(md["text"].(string), "### 标题") {
		t.Errorf("markdown = %v", md)
	}

	gotBody = nil
	if err := n.Send(context.Background(), map[string]any{"webhook_url": "http://x"}, &Message{Title: "标题", Body: "正文"}); err != nil {
		t.Fatal(err)
	}
	if gotBody["msgtype"] != "text" {
		t.Fatalf("msgtype = %v, want text", gotBody["msgtype"])
	}
}
