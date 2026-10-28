package notifier

// metaEN 是内置通知渠道的英文元数据；结构与 source/meta_i18n.go 一致。
type channelFieldEN struct {
	label       string
	help        string
	placeholder string
}

type channelMetaTranslation struct {
	label  string
	fields map[string]channelFieldEN
}

var metaEN = map[string]channelMetaTranslation{
	"webhook": {
		label: "Generic Webhook",
		fields: map[string]channelFieldEN{
			"url":      {label: "URL"},
			"secret":   {label: "Bearer token", help: "Optional; sent as Authorization: Bearer"},
			"markdown": {label: "Body as Markdown"},
		},
	},
	"telegram": {
		label: "Telegram",
		fields: map[string]channelFieldEN{
			"bot_token": {label: "Bot token", help: "Get it from BotFather"},
			"chat_id":   {label: "Chat ID", help: "Use @channelname or a numeric ID for channels"},
		},
	},
	"discord": {
		label: "Discord",
		fields: map[string]channelFieldEN{
			"webhook_url": {label: "Webhook URL", help: "Channel settings → Integrations → Webhooks"},
		},
	},
	"slack": {
		label: "Slack",
		fields: map[string]channelFieldEN{
			"webhook_url": {label: "Incoming Webhook URL"},
		},
	},
	"ntfy": {
		label: "ntfy",
		fields: map[string]channelFieldEN{
			"server": {label: "Server URL", help: "Defaults to https://ntfy.sh"},
			"topic":  {label: "Topic"},
			"token":  {label: "Access token", help: "Set for private topics"},
		},
	},
	"gotify": {
		label: "Gotify",
		fields: map[string]channelFieldEN{
			"server": {label: "Server URL"},
			"token":  {label: "App token"},
		},
	},
	"feishu": {
		label: "Feishu",
		fields: map[string]channelFieldEN{
			"webhook_url": {label: "Bot webhook", help: "Group settings → Group bots → Custom bot"},
		},
	},
	"wecom": {
		label: "WeCom",
		fields: map[string]channelFieldEN{
			"webhook_url": {label: "Group bot webhook"},
		},
	},
	"dingtalk": {
		label: "DingTalk",
		fields: map[string]channelFieldEN{
			"webhook_url": {label: "Bot webhook", help: "Use a custom keyword in security settings and include RelayWatch"},
		},
	},
	"pushdeer": {
		label: "PushDeer",
		fields: map[string]channelFieldEN{
			"server": {label: "Server URL", help: "Defaults to https://api2.pushdeer.com"},
			"key":    {label: "Push key"},
		},
	},
	"serverchan": {
		label: "ServerChan",
		fields: map[string]channelFieldEN{
			"sendkey": {label: "SendKey", help: "Get it at sct.ftqq.com; looks like SCTxxxx"},
		},
	},
	"bark": {
		label: "Bark",
		fields: map[string]channelFieldEN{
			"server": {label: "Server URL", help: "Self-hosted or official"},
			"key":    {label: "Device key"},
		},
	},
	"email_smtp": {
		label: "Email (SMTP)",
		fields: map[string]channelFieldEN{
			"host":       {label: "SMTP server"},
			"port":       {label: "Port"},
			"encryption": {label: "Encryption", help: "Leave empty to auto-select: SSL for 465, STARTTLS for 587; none is plaintext and not recommended"},
			"username":   {label: "Username"},
			"password":   {label: "Password / app password"},
			"from":       {label: "From"},
			"to":         {label: "To", help: "Comma-separated; leave empty to send to the channel owner's account email"},
		},
	},
}

func applyMetaEN(m *Meta) {
	tr, ok := metaEN[m.Type]
	if !ok {
		return
	}
	m.LabelEn = tr.label
	for i := range m.Fields {
		f := &m.Fields[i]
		ft, ok := tr.fields[f.Key]
		if !ok {
			continue
		}
		f.LabelEn = ft.label
		f.HelpEn = ft.help
		f.PlaceholderEn = ft.placeholder
	}
}
