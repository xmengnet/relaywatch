package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/smtp"
	"strings"
)

// ---------- Discord ----------

// DiscordNotifier Discord Webhook。
type DiscordNotifier struct{}

func (n *DiscordNotifier) Type() string { return "discord" }

func (n *DiscordNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "Discord",
		Icon:  "mdi-discord",
		Fields: []FieldSpec{
			{Key: "webhook_url", Label: "Webhook URL", Type: "secret", Required: true, Help: "频道设置 → 整合 → Webhook"},
		},
	}
}

type discordPayload struct {
	Content string `json:"content"`
}

func (n *DiscordNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "webhook_url")
}

func (n *DiscordNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	body, _ := json.Marshal(discordPayload{Content: msg.Title + "\n" + msg.Body})
	status, resp, err := httpPost(ctx, cfgString(cfg, "webhook_url"), nil, body)
	if err != nil {
		return fmt.Errorf("discord: %w", err)
	}
	return checkStatus(status, resp, "discord")
}

// ---------- Slack ----------

// SlackNotifier Slack Incoming Webhook。
type SlackNotifier struct{}

func (n *SlackNotifier) Type() string { return "slack" }

func (n *SlackNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "Slack",
		Icon:  "mdi-slack",
		Fields: []FieldSpec{
			{Key: "webhook_url", Label: "Incoming Webhook URL", Type: "secret", Required: true},
		},
	}
}

type slackPayload struct {
	Text string `json:"text"`
}

func (n *SlackNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "webhook_url")
}

func (n *SlackNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	body, _ := json.Marshal(slackPayload{Text: msg.Title + "\n" + msg.Body})
	status, resp, err := httpPost(ctx, cfgString(cfg, "webhook_url"), nil, body)
	if err != nil {
		return fmt.Errorf("slack: %w", err)
	}
	return checkStatus(status, resp, "slack")
}

// ---------- ntfy ----------

// NtfyNotifier ntfy.sh / 自建 ntfy。
type NtfyNotifier struct{}

func (n *NtfyNotifier) Type() string { return "ntfy" }

func (n *NtfyNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "ntfy",
		Icon:  "mdi-bell",
		Fields: []FieldSpec{
			{Key: "server", Label: "服务地址", Type: "string", Help: "默认 https://ntfy.sh"},
			{Key: "topic", Label: "Topic", Type: "string", Required: true},
			{Key: "token", Label: "Access Token", Type: "secret", Help: "私有 topic 时填写"},
		},
	}
}

func (n *NtfyNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "topic")
}

func (n *NtfyNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	server := cfgString(cfg, "server")
	if server == "" {
		server = "https://ntfy.sh"
	}
	server = strings.TrimRight(server, "/")
	headers := map[string]string{
		"Title":    msg.Title,
		"Priority": "default",
		"Tags":     "arrow-up-circle",
	}
	if msg.Markdown {
		headers["Markdown"] = "yes"
	}
	if tok := cfgString(cfg, "token"); tok != "" {
		headers["Authorization"] = "Bearer " + tok
	}
	status, resp, err := httpPost(ctx, server+"/"+cfgString(cfg, "topic"), headers, []byte(msg.Body))
	if err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	return checkStatus(status, resp, "ntfy")
}

// ---------- Gotify ----------

// GotifyNotifier 自建 Gotify。
type GotifyNotifier struct{}

func (n *GotifyNotifier) Type() string { return "gotify" }

func (n *GotifyNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "Gotify",
		Icon:  "mdi-bell-badge",
		Fields: []FieldSpec{
			{Key: "server", Label: "服务地址", Type: "string", Required: true, Placeholder: "https://gotify.example.com"},
			{Key: "token", Label: "App Token", Type: "secret", Required: true},
		},
	}
}

type gotifyPayload struct {
	Title    string `json:"title"`
	Message  string `json:"message"`
	Priority int    `json:"priority"`
}

func (n *GotifyNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "server", "token")
}

func (n *GotifyNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	server := strings.TrimRight(cfgString(cfg, "server"), "/")
	body, _ := json.Marshal(gotifyPayload{Title: msg.Title, Message: msg.Body, Priority: 5})
	url := fmt.Sprintf("%s/message?token=%s", server, cfgString(cfg, "token"))
	status, resp, err := httpPost(ctx, url, nil, body)
	if err != nil {
		return fmt.Errorf("gotify: %w", err)
	}
	return checkStatus(status, resp, "gotify")
}

// ---------- 飞书 ----------

// FeishuNotifier 飞书自定义机器人。
type FeishuNotifier struct{}

func (n *FeishuNotifier) Type() string { return "feishu" }

func (n *FeishuNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "飞书",
		Icon:  "mdi-briefcase",
		Fields: []FieldSpec{
			{Key: "webhook_url", Label: "机器人 Webhook", Type: "secret", Required: true, Help: "群设置 → 群机器人 → 自定义机器人"},
		},
	}
}

type feishuPayload struct {
	MsgType string        `json:"msg_type"`
	Content feishuContent `json:"content"`
}

type feishuContent struct {
	Text string `json:"text"`
}

func (n *FeishuNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "webhook_url")
}

func (n *FeishuNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	body, _ := json.Marshal(feishuPayload{
		MsgType: "text",
		Content: feishuContent{Text: msg.Title + "\n" + msg.Body},
	})
	status, resp, err := httpPost(ctx, cfgString(cfg, "webhook_url"), nil, body)
	if err != nil {
		return fmt.Errorf("feishu: %w", err)
	}
	// 飞书 200 也可能带业务错误码，检查响应体
	if err := checkStatus(status, resp, "feishu"); err != nil {
		return err
	}
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(resp, &r)
	if r.Code != 0 {
		return fmt.Errorf("feishu: %s", r.Msg)
	}
	return nil
}

// ---------- 企业微信 ----------

// WecomNotifier 企业微信群机器人。
type WecomNotifier struct{}

func (n *WecomNotifier) Type() string { return "wecom" }

func (n *WecomNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "企业微信",
		Icon:  "mdi-wechat",
		Fields: []FieldSpec{
			{Key: "webhook_url", Label: "群机器人 Webhook", Type: "secret", Required: true},
		},
	}
}

type wecomPayload struct {
	MsgType string      `json:"msgtype"`
	Text    wecomText   `json:"text"`
}

type wecomText struct {
	Content string `json:"content"`
}

func (n *WecomNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "webhook_url")
}

func (n *WecomNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	body, _ := json.Marshal(wecomPayload{
		MsgType: "text",
		Text:    wecomText{Content: msg.Title + "\n" + msg.Body},
	})
	status, resp, err := httpPost(ctx, cfgString(cfg, "webhook_url"), nil, body)
	if err != nil {
		return fmt.Errorf("wecom: %w", err)
	}
	if err := checkStatus(status, resp, "wecom"); err != nil {
		return err
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	_ = json.Unmarshal(resp, &r)
	if r.ErrCode != 0 {
		return fmt.Errorf("wecom: %s", r.ErrMsg)
	}
	return nil
}

// ---------- 钉钉 ----------

// DingTalkNotifier 钉钉群机器人。
type DingTalkNotifier struct{}

func (n *DingTalkNotifier) Type() string { return "dingtalk" }

func (n *DingTalkNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "钉钉",
		Icon:  "mdi-robot-outline",
		Fields: []FieldSpec{
			{Key: "webhook_url", Label: "机器人 Webhook", Type: "secret", Required: true, Help: "安全设置建议选「自定义关键词」，加 RelayWatch"},
		},
	}
}

type dingPayload struct {
	MsgType string    `json:"msgtype"`
	Text    dingText  `json:"text"`
}

type dingText struct {
	Content string `json:"content"`
}

func (n *DingTalkNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "webhook_url")
}

func (n *DingTalkNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	body, _ := json.Marshal(dingPayload{
		MsgType: "text",
		Text:    dingText{Content: "RelayWatch\n" + msg.Title + "\n" + msg.Body},
	})
	status, resp, err := httpPost(ctx, cfgString(cfg, "webhook_url"), nil, body)
	if err != nil {
		return fmt.Errorf("dingtalk: %w", err)
	}
	if err := checkStatus(status, resp, "dingtalk"); err != nil {
		return err
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	_ = json.Unmarshal(resp, &r)
	if r.ErrCode != 0 {
		return fmt.Errorf("dingtalk: %s", r.ErrMsg)
	}
	return nil
}

// ---------- PushDeer / Server酱 / Bark ----------

// PushDeerNotifier PushDeer。
type PushDeerNotifier struct{}

func (n *PushDeerNotifier) Type() string { return "pushdeer" }

func (n *PushDeerNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "PushDeer",
		Icon:  "mdi-deer",
		Fields: []FieldSpec{
			{Key: "server", Label: "服务地址", Type: "string", Help: "默认 https://api2.pushdeer.com"},
			{Key: "key", Label: "Push Key", Type: "secret", Required: true},
		},
	}
}

func (n *PushDeerNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "key")
}

func (n *PushDeerNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	server := strings.TrimRight(cfgString(cfg, "server"), "/")
	if server == "" {
		server = "https://api2.pushdeer.com"
	}
	body, _ := json.Marshal(map[string]any{
		"pushkey": cfgString(cfg, "key"),
		"text":    msg.Title,
		"desp":    msg.Body,
		"type":    "markdown",
	})
	status, resp, err := httpPost(ctx, server+"/message/push", nil, body)
	if err != nil {
		return fmt.Errorf("pushdeer: %w", err)
	}
	return checkStatus(status, resp, "pushdeer")
}

// ServerChanNotifier Server酱。
type ServerChanNotifier struct{}

func (n *ServerChanNotifier) Type() string { return "serverchan" }

func (n *ServerChanNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "Server酱",
		Icon:  "mdi-turkey",
		Fields: []FieldSpec{
			{Key: "sendkey", Label: "SendKey", Type: "secret", Required: true, Help: "sct.ftqq.com 获取，形如 SCTxxxx"},
		},
	}
}

func (n *ServerChanNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "sendkey")
}

func (n *ServerChanNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	key := cfgString(cfg, "sendkey")
	body, _ := json.Marshal(map[string]any{
		"title": msg.Title,
		"desp":  msg.Body,
	})
	status, resp, err := httpPost(ctx, fmt.Sprintf("https://sctapi.ftqq.com/%s.send", key), nil, body)
	if err != nil {
		return fmt.Errorf("serverchan: %w", err)
	}
	return checkStatus(status, resp, "serverchan")
}

// BarkNotifier Bark（iOS 推送）。
type BarkNotifier struct{}

func (n *BarkNotifier) Type() string { return "bark" }

func (n *BarkNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "Bark",
		Icon:  "mdi-cellphone-arrow-down",
		Fields: []FieldSpec{
			{Key: "server", Label: "服务地址", Type: "string", Required: true, Placeholder: "https://api.day.app", Help: "自建或官方"},
			{Key: "key", Label: "Device Key", Type: "secret", Required: true},
		},
	}
}

type barkPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (n *BarkNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "server", "key")
}

func (n *BarkNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	server := strings.TrimRight(cfgString(cfg, "server"), "/")
	body, _ := json.Marshal(barkPayload{Title: msg.Title, Body: msg.Body})
	url := fmt.Sprintf("%s/push", server)
	headers := map[string]string{}
	if key := cfgString(cfg, "key"); key != "" {
		// 官方 API 用 key 路径或 query；JSON body 不接受 key，放 query
		url += "?device_key=" + key
	}
	status, resp, err := httpPost(ctx, url, headers, body)
	if err != nil {
		return fmt.Errorf("bark: %w", err)
	}
	return checkStatus(status, resp, "bark")
}

// ---------- SMTP ----------

// SMTPNotifier 邮件通知。
type SMTPNotifier struct{}

func (n *SMTPNotifier) Type() string { return "email_smtp" }

func (n *SMTPNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "邮件（SMTP）",
		Icon:  "mdi-email",
		Fields: []FieldSpec{
			{Key: "host", Label: "SMTP 服务器", Type: "string", Required: true, Placeholder: "smtp.example.com"},
			{Key: "port", Label: "端口", Type: "string", Required: true, Placeholder: "465 或 587"},
			{Key: "encryption", Label: "加密方式", Type: "select", Options: []string{"", "ssl", "starttls", "none"}, Help: "留空自动：465 走 SSL，587 走 STARTTLS；none 为明文，不推荐"},
			{Key: "username", Label: "账号", Type: "string", Required: true},
			{Key: "password", Label: "密码/授权码", Type: "secret", Required: true},
			{Key: "from", Label: "发件人", Type: "string", Required: true},
			{Key: "to", Label: "收件人", Type: "string", Required: true, Help: "多个用逗号分隔"},
		},
	}
}

func (n *SMTPNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "host", "port", "username", "password", "from", "to")
}

func (n *SMTPNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	from := cfgString(cfg, "from")
	recipients := strings.Split(cfgString(cfg, "to"), ",")
	for i := range recipients {
		recipients[i] = strings.TrimSpace(recipients[i])
	}
	content := strings.Join([]string{
		"From: " + from,
		"To: " + strings.Join(recipients, ", "),
		"Subject: " + msg.Title,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		msg.Body,
	}, "\r\n")

	port := cfgString(cfg, "port")
	addr := cfgString(cfg, "host") + ":" + port
	host := cfgString(cfg, "host")
	auth := smtp.PlainAuth("", cfgString(cfg, "username"), cfgString(cfg, "password"), host)

	// 加密方式：显式指定优先；留空按端口推断（465 SSL，其余 STARTTLS）
	encryption := cfgString(cfg, "encryption")
	switch encryption {
	case "ssl":
		return sendSMTPS(addr, host, auth, from, recipients, []byte(content))
	case "none":
		return sendSMTPPlain(ctx, addr, auth, from, recipients, []byte(content))
	case "starttls":
		return sendSMTPStartTLS(ctx, addr, host, auth, from, recipients, []byte(content))
	}
	if port == "465" {
		return sendSMTPS(addr, host, auth, from, recipients, []byte(content))
	}
	return sendSMTPStartTLS(ctx, addr, host, auth, from, recipients, []byte(content))
}
