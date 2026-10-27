package notifier

// NewDefaultRegistry 返回注册了全部内置渠道的注册表。
// 新增渠道类型：实现 Notifier 后在此追加一行即可，main 无需改动。
func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(&WebhookNotifier{})
	r.Register(&TelegramNotifier{})
	r.Register(&DiscordNotifier{})
	r.Register(&SlackNotifier{})
	r.Register(&NtfyNotifier{})
	r.Register(&GotifyNotifier{})
	r.Register(&FeishuNotifier{})
	r.Register(&WecomNotifier{})
	r.Register(&DingTalkNotifier{})
	r.Register(&PushDeerNotifier{})
	r.Register(&ServerChanNotifier{})
	r.Register(&BarkNotifier{})
	r.Register(&SMTPNotifier{})
	return r
}
