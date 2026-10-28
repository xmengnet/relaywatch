package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"relaywatch/internal/notifier"
)

// ---------- 全局邮件配置 ----------

func TestMailConfigAuthAndCRUD(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "admin-ac", "admin", "password123")
	env.mustUser(t, "user1", "user", "password123")
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")
	uC, uCSRF := env.loginAs(t, "user1", "password123")

	// 普通用户读写均 403
	rec := env.do(t, "GET", "/api/v1/mail-config", uC, uCSRF, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("user GET mail-config = %d, want 403", rec.Code)
	}
	rec = env.do(t, "PUT", "/api/v1/mail-config", uC, uCSRF, map[string]any{"host": "smtp.x", "from": "a@x"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("user PUT mail-config = %d, want 403", rec.Code)
	}

	// admin 读取默认（未配置）
	rec = env.do(t, "GET", "/api/v1/mail-config", adminC, adminCSRF, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"password_set":true`) {
		t.Errorf("empty config should report password_set=false")
	}

	// 保存配置（带密码）
	rec = env.do(t, "PUT", "/api/v1/mail-config", adminC, adminCSRF, map[string]any{
		"host": "smtp.example.com", "port": "587", "username": "u", "password": "password123", "from": "rw@example.com", "encryption": "starttls",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin PUT = %d %s", rec.Code, rec.Body.String())
	}

	// 回读：密码不回传，password_set=true
	rec = env.do(t, "GET", "/api/v1/mail-config", adminC, adminCSRF, nil)
	body := rec.Body.String()
	if strings.Contains(body, "password123") {
		t.Errorf("password must not be returned, got %s", body)
	}
	if !strings.Contains(body, `"password_set":true`) {
		t.Errorf("password_set should be true, got %s", body)
	}

	// 再次保存：密码留空沿用旧值
	rec = env.do(t, "PUT", "/api/v1/mail-config", adminC, adminCSRF, map[string]any{
		"host": "smtp2.example.com", "port": "465", "from": "rw@example.com", "encryption": "ssl",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update without password = %d", rec.Code)
	}
	_, pw, err := (&MailConfigHandler{St: env.st}).load(context.Background())
	if err != nil || pw == "" {
		t.Errorf("password should be preserved, got %q err=%v", pw, err)
	}

	// 非法 encryption → 400
	rec = env.do(t, "PUT", "/api/v1/mail-config", adminC, adminCSRF, map[string]any{"host": "x", "from": "a@x", "encryption": "bogus"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad encryption = %d, want 400", rec.Code)
	}
}

// ---------- 注册邮箱验证码 ----------

func TestRegisterEmailCodeFlow(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "admin-ac", "admin", "password123")
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")

	// 开注册 + 需邮箱验证
	rec := env.do(t, "PUT", "/api/v1/settings", adminC, adminCSRF, map[string]any{
		"registration_enabled": true, "registration_default_role": "user", "registration_require_email": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: %d", rec.Code)
	}

	// 未配置邮件 → 发验证码 503
	rec = env.do(t, "POST", "/api/v1/auth/register/send-code", "", "", map[string]any{"email": "new@example.com"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("send-code without mail = %d, want 503", rec.Code)
	}

	// 配置全局邮件（指向本地假 SMTP 地址，发送会失败但我们走"注入码"路径测试注册）
	rec = env.do(t, "PUT", "/api/v1/mail-config", adminC, adminCSRF, map[string]any{
		"host": "127.0.0.1", "port": "1", "from": "rw@example.com", "encryption": "none",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("mail config: %d", rec.Code)
	}

	// 生成验证码（直接走内部函数，模拟邮件已送达）
	code, err := GenerateRegCode(context.Background(), env.st, "new@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// 错误码 → 400
	rec = env.do(t, "POST", "/api/v1/auth/register", "", "", map[string]any{
		"username": "newbie", "password": "password123", "email": "new@example.com", "email_code": "000000",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("wrong code = %d, want 400", rec.Code)
	}
	// 邮箱不匹配 → 400
	code2, _ := GenerateRegCode(context.Background(), env.st, "other@example.com")
	rec = env.do(t, "POST", "/api/v1/auth/register", "", "", map[string]any{
		"username": "newbie", "password": "password123", "email": "new@example.com", "email_code": code2,
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("mismatched email = %d, want 400", rec.Code)
	}
	code, _ = GenerateRegCode(context.Background(), env.st, "new@example.com") // 上一步未消费，重新生成防过期混乱

	// 正确码 → 注册成功
	rec = env.do(t, "POST", "/api/v1/auth/register", "", "", map[string]any{
		"username": "newbie", "password": "password123", "email": "new@example.com", "email_code": code,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register with code = %d %s", rec.Code, rec.Body.String())
	}

	// 验证码一次性：复用 → 400
	rec = env.do(t, "POST", "/api/v1/auth/register", "", "", map[string]any{
		"username": "newbie2", "password": "password123", "email": "new@example.com", "email_code": code,
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("reused code = %d, want 400", rec.Code)
	}
}

func TestVerifyRegCodeExpiry(t *testing.T) {
	env := newMultiUserEnv(t)
	ctx := context.Background()
	code, err := GenerateRegCode(ctx, env.st, "a@x.io")
	if err != nil {
		t.Fatal(err)
	}
	// 手动把过期时间改到过去
	raw, _ := env.st.GetSetting(ctx, "registration.email_code")
	future := strings.Replace(raw, time.Now().UTC().Format("2006"), "2000", 1)
	_ = env.st.SetSetting(ctx, "registration.email_code", future)
	if err := VerifyRegCode(ctx, env.st, "a@x.io", code); err == nil || !strings.Contains(err.Error(), "过期") {
		t.Errorf("expired code should fail with 过期, got %v", err)
	}
}

// SendSystemMailRaw 基础冒烟：非法地址返回错误而非 panic。
func TestSendSystemMailRawSmoke(t *testing.T) {
	err := notifier.SendSystemMailRaw(context.Background(), "127.0.0.1:1", "127.0.0.1", "none", nil, "a@x", []string{"b@x"}, []byte("hi"))
	if err == nil {
		t.Skip("unexpected success on closed port")
	}
}
