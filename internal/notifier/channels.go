package notifier

import (
	"context"
	"encoding/json"
	"fmt"
)

// WebhookSource 通用 HTTP POST webhook。
type WebhookNotifier struct{}

func (n *WebhookNotifier) Type() string { return "webhook" }

func (n *WebhookNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "通用 Webhook",
		Icon:  "mdi-webhook",
		Fields: []FieldSpec{
			{Key: "url", Label: "URL", Type: "string", Required: true, Placeholder: "https://example.com/hook"},
			{Key: "secret", Label: "Bearer Token", Type: "secret", Help: "可选，作为 Authorization: Bearer 发送"},
			{Key: "markdown", Label: "正文用 Markdown", Type: "bool"},
		},
	}
}

type webhookPayload struct {
	Title    string       `json:"title"`
	Body     string       `json:"body"`
	URL      string       `json:"url,omitempty"`
	Markdown bool         `json:"markdown"`
	Monitor  webhookMon   `json:"monitor"`
	Change   *webhookChg  `json:"change,omitempty"`
}

type webhookMon struct {
	Name       string   `json:"name"`
	SourceType string   `json:"source_type"`
	Tags       []string `json:"tags,omitempty"`
}

type webhookChg struct {
	OldVersion string `json:"old_version,omitempty"`
	NewVersion string `json:"new_version"`
}

func (n *WebhookNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "url")
}

func (n *WebhookNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	url := cfgString(cfg, "url")
	p := webhookPayload{
		Title:    msg.Title,
		Body:     msg.Body,
		URL:      msg.URL,
		Markdown: msg.Markdown || cfgBool(cfg, "markdown"),
		Monitor:  webhookMon{Name: msg.Monitor.Name, SourceType: msg.Monitor.SourceType, Tags: msg.Monitor.Tags},
	}
	if msg.Change.NewVersion != "" {
		p.Change = &webhookChg{OldVersion: msg.Change.OldVersion, NewVersion: msg.Change.NewVersion}
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	headers := map[string]string{}
	if tok := cfgString(cfg, "secret"); tok != "" {
		headers["Authorization"] = "Bearer " + tok
	}
	status, respBody, err := httpPost(ctx, url, headers, body)
	if err != nil {
		return fmt.Errorf("webhook: %w", err)
	}
	return checkStatus(status, respBody, "webhook")
}

// TelegramNotifier Telegram Bot API。
type TelegramNotifier struct{}

func (n *TelegramNotifier) Type() string { return "telegram" }

func (n *TelegramNotifier) Meta() Meta {
	return Meta{
		Type:  n.Type(),
		Label: "Telegram",
		Icon:  "mdi-telegram",
		Fields: []FieldSpec{
			{Key: "bot_token", Label: "Bot Token", Type: "secret", Required: true, Help: "从 @BotFather 获取"},
			{Key: "chat_id", Label: "Chat ID", Type: "string", Required: true, Help: "频道用 @channelname 或数字 ID"},
		},
	}
}

type tgRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

func (n *TelegramNotifier) Validate(cfg map[string]any) error {
	return required(cfg, "bot_token", "chat_id")
}

func (n *TelegramNotifier) Send(ctx context.Context, cfg map[string]any, msg *Message) error {
	token := cfgString(cfg, "bot_token")
	text := msg.Body
	req := tgRequest{ChatID: cfgString(cfg, "chat_id"), Text: text}
	if msg.Markdown {
		req.ParseMode = "Markdown"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	status, respBody, err := httpPost(ctx, url, nil, body)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	return checkStatus(status, respBody, "telegram")
}
