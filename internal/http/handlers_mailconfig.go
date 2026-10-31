package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"relaywatch/internal/notifier"
	"relaywatch/internal/store"
)

// MailConfigHandler 全局邮件（SMTP）配置：系统邮件（注册验证码等）的
// 独立发件通道，与通知渠道无关。仅 admin 可读写。
type MailConfigHandler struct {
	St  store.Store
	Box store.SecretBox // 敏感字段加解密（nil = 明文，仅测试）
}

const (
	settingMailConfig   = "mail.smtp" // JSON（password 字段 AES-GCM 加密）
	settingMailPassword = "mail.smtp.password_enc"
)

func (h *MailConfigHandler) routes(r chiRouterIface) {
	r.Get("/mail-config", h.get)
	r.Put("/mail-config", h.put)
	r.Post("/mail-config/test", h.test)
}

type mailConfig struct {
	Host       string `json:"host"`
	Port       string `json:"port"`
	Username   string `json:"username"`
	From       string `json:"from"`
	Encryption string `json:"encryption"` // starttls | ssl | none
}

type storedMailConfig struct {
	mailConfig
	PasswordEnc string `json:"password_enc"` // 落库前由 secretbox 加密
}

func (h *MailConfigHandler) load(ctx context.Context) (*mailConfig, string, error) {
	raw, err := h.St.GetSetting(ctx, settingMailConfig)
	if err != nil || raw == "" {
		return nil, "", store.ErrNotFound
	}
	var sc storedMailConfig
	if err := json.Unmarshal([]byte(raw), &sc); err != nil {
		return nil, "", err
	}
	pw := sc.PasswordEnc
	if h.Box != nil && pw != "" {
		plain, err := h.Box.Decrypt(pw)
		if err != nil {
			return nil, "", fmt.Errorf("邮件密码解密失败: %w", err)
		}
		pw = plain
	}
	return &sc.mailConfig, pw, nil
}

func (h *MailConfigHandler) get(w http.ResponseWriter, r *http.Request) {
	cfg, _, err := h.load(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"config": mailConfig{Port: "587", Encryption: "starttls"}, "password_set": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config":       cfg,
		"password_set": h.passwordSet(r.Context()),
	})
}

func (h *MailConfigHandler) passwordSet(ctx context.Context) bool {
	_, pw, err := h.load(ctx)
	return err == nil && pw != ""
}

func (h *MailConfigHandler) put(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host       string  `json:"host"`
		Port       string  `json:"port"`
		Username   string  `json:"username"`
		Password   *string `json:"password"` // nil/空 = 不修改
		From       string  `json:"from"`
		Encryption string  `json:"encryption"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Host) == "" || strings.TrimSpace(req.From) == "" {
		writeError(w, http.StatusBadRequest, "SMTP 主机与发件人地址必填")
		return
	}
	switch req.Encryption {
	case "starttls", "ssl", "none", "":
		if req.Encryption == "" {
			req.Encryption = "starttls"
		}
	default:
		writeError(w, http.StatusBadRequest, "encryption 只能是 starttls/ssl/none")
		return
	}

	sc := storedMailConfig{mailConfig: mailConfig{
		Host: strings.TrimSpace(req.Host), Port: req.Port,
		Username: req.Username, From: strings.TrimSpace(req.From),
		Encryption: req.Encryption,
	}}
	if req.Password != nil && *req.Password != "" {
		sc.PasswordEnc = *req.Password
		if h.Box != nil {
			enc, err := h.Box.Encrypt(*req.Password)
			if err != nil {
				slog.Error("mail config password encrypt failed, storing plaintext", "err", err)
			} else {
				sc.PasswordEnc = enc
			}
		}
	} else if _, oldPw, err := h.load(r.Context()); err == nil && oldPw != "" {
		// 留空 = 不修改：沿用旧密文（load 已解密，重新加密）
		sc.PasswordEnc = oldPw
		if h.Box != nil {
			enc, err := h.Box.Encrypt(oldPw)
			if err != nil {
				slog.Error("mail config password re-encrypt failed", "err", err)
			} else {
				sc.PasswordEnc = enc
			}
		}
	}
	raw, _ := json.Marshal(sc)
	if err := h.St.SetSetting(r.Context(), settingMailConfig, string(raw)); err != nil {
		writeInternalError(w, err)
		return
	}
	u := authUser(r)
	_ = h.St.AddAudit(r.Context(), &store.AuditEntry{UserID: &u.ID, Action: "mail_config.update", IP: clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// test 用当前保存的配置发一封测试邮件到当前用户资料里的邮箱。
// 未维护邮箱时报错：发给 From（noreply 发件账号）的测试对使用者毫无验证价值。
func (h *MailConfigHandler) test(w http.ResponseWriter, r *http.Request) {
	u := authUser(r)
	got, err := h.St.UserByID(r.Context(), u.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	to := strings.TrimSpace(got.Email)
	if to == "" {
		writeError(w, http.StatusBadRequest, "当前账户未维护邮箱，请先在「个人资料」中填写")
		return
	}
	// 收件人直拼 SMTP 信封与 To 头，带 CRLF 的值可注入头部；格式不对直接拒绝
	if addr, err := mail.ParseAddress(to); err != nil || addr.Address != to {
		writeError(w, http.StatusBadRequest, "当前账户邮箱格式不正确，请先在「个人资料」中修正")
		return
	}
	cfg, pw, err := h.load(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, "尚未配置邮件")
		return
	}
	if err := sendSystemMail(r.Context(), cfg, pw, to, "RelayWatch 测试邮件", "全局邮件配置正确。这是一封来自 RelayWatch 的测试邮件。"); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "to": to})
}

// ---------- 系统邮件发送与验证码（供注册流程使用） ----------

// SendSystemMail 用当前保存的配置发送系统邮件（注册验证码等）。
// 必须走同一个 handler 实例，才能带上 SecretBox 解密 SMTP 密码。
func (h *MailConfigHandler) SendSystemMail(ctx context.Context, to, subject, body string) error {
	cfg, pw, err := h.load(ctx)
	if err != nil {
		return fmt.Errorf("全局邮件未配置")
	}
	return sendSystemMail(ctx, cfg, pw, to, subject, body)
}

func sendSystemMail(ctx context.Context, cfg *mailConfig, password, to, subject, body string) error {
	addr := cfg.Host + ":" + cfg.Port
	content := strings.Join([]string{
		"From: " + sanitizeMailHeader(cfg.From),
		"To: " + sanitizeMailHeader(to),
		"Subject: " + sanitizeMailHeader(subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}, "\r\n")
	var auth smtp.Auth
	if cfg.Username != "" && password != "" {
		auth = smtp.PlainAuth("", cfg.Username, password, cfg.Host)
	}
	return notifier.SendSystemMailRaw(ctx, addr, cfg.Host, cfg.Encryption, auth, cfg.From, []string{to}, []byte(content))
}

// sanitizeMailHeader 把邮件头值中的 CR/LF 替换为空格，阻断头部注入。
func sanitizeMailHeader(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, s)
}

// ---------- 注册邮箱验证码 ----------

// GenerateRegCode 生成 6 位验证码并落库（10 分钟有效），返回明文。
func GenerateRegCode(ctx context.Context, st store.Store, email string) (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", (int(b[0])<<16|int(b[1])<<8|int(b[2]))%1000000)
	now := time.Now()
	if err := st.PutRegistrationCode(ctx, email, code, now.Add(10*time.Minute), now); err != nil {
		return "", err
	}
	_ = st.SweepRegistrationCodes(ctx)
	return code, nil
}

// regCodeMaxAttempts 单个验证码允许的最大错误次数，超过即作废。
// 6 位数字码 10 分钟有效，无上限时可被在线枚举爆破。
const regCodeMaxAttempts = 5

// VerifyRegCode 校验并原子消费验证码（一次性，按邮箱隔离）。
// 连续错误达到上限后作废，需重新获取（新码发送走邮箱，邮箱主人可感知）。
func VerifyRegCode(ctx context.Context, st store.Store, email, code string) error {
	rec, ok, err := st.GetRegistrationCode(ctx, email)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("请先获取验证码")
	}
	if time.Now().After(rec.ExpiresAt) {
		return fmt.Errorf("验证码已过期，请重新获取")
	}
	if rec.Attempts >= regCodeMaxAttempts {
		_ = st.DeleteRegistrationCode(ctx, email)
		return fmt.Errorf("错误次数过多，验证码已作废，请重新获取")
	}
	if rec.Code != code {
		if n, err := st.BumpRegistrationCodeAttempts(ctx, email); err == nil && n >= regCodeMaxAttempts {
			_ = st.DeleteRegistrationCode(ctx, email)
		}
		return fmt.Errorf("验证码不正确")
	}
	consumed, err := st.ConsumeRegistrationCode(ctx, email, code, time.Now())
	if err != nil {
		return err
	}
	if !consumed {
		return fmt.Errorf("验证码已失效，请重新获取")
	}
	return nil
}
