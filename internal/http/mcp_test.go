package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"relaywatch/internal/auth"
	httpapi "relaywatch/internal/http"
	mcpserver "relaywatch/internal/mcp"
	"relaywatch/internal/notifier"
	"relaywatch/internal/source"
	"relaywatch/internal/store"
)

// mcpEnv 构造挂载了 MCP 端点的完整 HTTP 服务（与生产 main 同构）。
// 返回服务、auth 服务与 store，供测试建多用户/多 token。
func mcpEnv(t *testing.T) (*httptest.Server, *auth.Service, store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(st, time.Hour)
	registry := source.NewDefaultRegistry()
	notifiers := notifier.NewDefaultRegistry()
	s := &httpapi.Server{
		Auth:      svc,
		Store:     st,
		Registry:  registry,
		Notifiers: notifiers,
		Version:   "test",
		MCP: mcpserver.NewHTTPHandler(mcpserver.New(mcpserver.Deps{
			St:        st,
			Sources:   registry,
			Notifiers: notifiers,
			Version:   "test",
		})),
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, svc, st
}

// newUserToken 建用户并签发 API Token。
func newUserToken(t *testing.T, svc *auth.Service, st store.Store, name, role string) string {
	t.Helper()
	u := &store.User{Username: name, Role: role, PasswordHash: mustHash(t, "TestPassw0rd!")}
	if err := st.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.CreateAPIToken(context.Background(), u.ID, "mcp-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func mustHash(t *testing.T, pw string) []byte {
	t.Helper()
	h, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// mcpSession 以 Bearer Token 建立真实 MCP 会话（Streamable HTTP 全链路）。
func mcpSession(t *testing.T, ts *httptest.Server, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	tr := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerRoundTripper{token: token}},
	}
	sess, err := client.Connect(context.Background(), tr, nil)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

type bearerRoundTripper struct{ token string }

func (b bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// 全链路：Bearer Token → MCP 握手 → tools/list → 工具调用。
func TestMCPEndToEnd(t *testing.T) {
	ts, svc, st := mcpEnv(t)
	adminToken := newUserToken(t, svc, st, "ai", "admin")
	sess := mcpSession(t, ts, adminToken)
	ctx := context.Background()

	// tools/list 应包含注册的工具
	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) < 10 {
		t.Fatalf("tools = %d, want >= 10", len(tools.Tools))
	}

	// 创建监控（create_monitor 走与 HTTP API 相同的校验链）
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "create_monitor",
		Arguments: map[string]any{
			"name":          "linux",
			"source_type":   "github_release",
			"source_config": map[string]any{"repo": "torvalds/linux"},
		},
	})
	if err != nil {
		t.Fatalf("create_monitor: %v", err)
	}
	if res.IsError {
		t.Fatalf("create_monitor tool error: %s", toolErrText(res))
	}

	// list_monitors 应能看到刚创建的监控
	res, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_monitors", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("list_monitors: %v", err)
	}
	var list struct {
		Items []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := unmarshalToolJSON(res, &list); err != nil {
		t.Fatalf("decode list_monitors: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].Name != "linux" {
		t.Fatalf("list_monitors = %+v, want 1 linux", list)
	}
	if list.Items[0].State != string(store.StateUnknown) {
		t.Fatalf("state = %s, want unknown", list.Items[0].State)
	}

	// 无效源类型应报错（共享校验链生效）
	res, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "create_monitor",
		Arguments: map[string]any{
			"name": "bad", "source_type": "nonexistent",
		},
	})
	if err != nil {
		t.Fatalf("call bad create_monitor: %v", err)
	}
	if !res.IsError {
		t.Fatalf("bad source_type should be a tool error, got %+v", res.Content)
	}
}

// 数据隔离：普通用户只能看到自己的监控；viewer 无权创建/写操作。
func TestMCPScopeAndPermissions(t *testing.T) {
	ts, svc, st := mcpEnv(t)
	ctx := context.Background()
	alice := mcpSession(t, ts, newUserToken(t, svc, st, "alice", "user"))
	bob := mcpSession(t, ts, newUserToken(t, svc, st, "bob", "user"))

	// alice 建监控
	res, err := alice.CallTool(ctx, &mcp.CallToolParams{
		Name: "create_monitor",
		Arguments: map[string]any{
			"name": "alice-mon", "source_type": "github_release",
			"source_config": map[string]any{"repo": "golang/go"},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("alice create: err=%v isError=%v text=%s", err, res.IsError, toolErrText(res))
	}

	// bob 看不到 alice 的监控
	res, err = bob.CallTool(ctx, &mcp.CallToolParams{Name: "list_monitors", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("bob list: %v", err)
	}
	var list struct {
		Total int `json:"total"`
	}
	if err := unmarshalToolJSON(res, &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if list.Total != 0 {
		t.Fatalf("bob sees %d monitors, want 0", list.Total)
	}

	// bob 直接访问 alice 的监控 ID → 工具错误（404 语义）
	var created struct {
		Item struct {
			ID int64 `json:"id"`
		} `json:"item"`
	}
	res, _ = alice.CallTool(ctx, &mcp.CallToolParams{Name: "list_monitors", Arguments: map[string]any{}})
	if err := unmarshalToolJSON(res, &created); err != nil {
		t.Fatalf("decode alice list: %v", err)
	}
	res, err = bob.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_monitor", Arguments: map[string]any{"id": created.Item.ID},
	})
	if err != nil || !res.IsError {
		t.Fatalf("bob get alice monitor: err=%v isError=%v（应报不存在）", err, res != nil && res.IsError)
	}

	// viewer 不能创建监控
	viewer := mcpSession(t, ts, newUserToken(t, svc, st, "carol", "viewer"))
	res, err = viewer.CallTool(ctx, &mcp.CallToolParams{
		Name: "create_monitor",
		Arguments: map[string]any{
			"name": "x", "source_type": "github_release",
			"source_config": map[string]any{"repo": "golang/go"},
		},
	})
	if err != nil || !res.IsError {
		t.Fatalf("viewer create should fail: err=%v isError=%v", err, res != nil && res.IsError)
	}
	if text := toolErrText(res); text != "没有权限：创建监控" {
		t.Fatalf("viewer error = %q", text)
	}
}

func unmarshalToolJSON(res *mcp.CallToolResult, v any) error {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return json.Unmarshal([]byte(tc.Text), v)
		}
	}
	return errors.New("tool result has no text content")
}

func toolErrText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return "(no text)"
}

// /SKILL.md 公开可读、含正确的实例地址替换，且不含任何真实凭据。
func TestSkillMDRoute(t *testing.T) {
	ts, _, _ := mcpEnv(t)
	res, err := http.Get(ts.URL + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /SKILL.md = %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Fatalf("content-type = %s, want text/markdown", ct)
	}
	body, _ := io.ReadAll(res.Body)
	for _, want := range []string{"name: relaywatch", "/mcp", "Authorization: Bearer", ts.URL} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("SKILL.md missing %q", want)
		}
	}
}

// /SKILL.md 大小写不敏感：任意大小写变体都返回同一文档。
func TestSkillMDCaseInsensitive(t *testing.T) {
	ts, _, _ := mcpEnv(t)
	for _, path := range []string{"/SKILL.md", "/skill.md", "/Skill.Md", "/SKILL.MD"} {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "name: relaywatch") {
			t.Fatalf("GET %s = %d（应 200 且返回 SKILL.md 内容）", path, res.StatusCode)
		}
	}
}
