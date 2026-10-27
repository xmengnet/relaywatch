package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"relaywatch/internal/auth"
	"relaywatch/internal/check"
	"relaywatch/internal/crypto"
	httpapi "relaywatch/internal/http"
	"relaywatch/internal/notifier"
	"relaywatch/internal/notify"
	"relaywatch/internal/scheduler"
	"relaywatch/internal/source"
	"relaywatch/internal/store"
	"relaywatch/web"
)

// release 由构建时 -ldflags 注入。
var release = "dev"

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	showVersion := flag.Bool("version", false, "打印版本并退出")
	flag.Parse()

	if *showVersion {
		fmt.Println("relaywatch", release)
		return
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fatal(err)
	}
	setupLogger(cfg)

	// 密钥：敏感字段加密必须
	secretKey := os.Getenv("RELAYWATCH_SECRET_KEY")
	if secretKey == "" {
		secretKey = cfg.Security.SecretKey
	}
	if secretKey == "" {
		fatal(fmt.Errorf("未设置 secret_key：请用 `openssl rand -base64 32` 生成，写入配置 security.secret_key 或环境变量 RELAYWATCH_SECRET_KEY"))
	}
	box, err := crypto.NewBox(secretKey, cfg.Security.OldSecretKey)
	if err != nil {
		fatal(err)
	}

	// 确保数据目录存在（必须在打开数据库前）
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o755); err != nil {
		fatal(fmt.Errorf("create data dir: %w", err))
	}

	st, err := store.Open(cfg.Database.Path)
	if err != nil {
		fatal(fmt.Errorf("open database: %w", err))
	}
	defer st.Close()
	st.SetSecretBox(box)

	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		fatal(fmt.Errorf("migrate: %w", err))
	}

	authSvc := auth.NewService(st, cfg.Security.SessionTTL)
	oidcSvc := auth.NewOIDCService(st, cfg.Security.SessionTTL, cfg.Server.BaseURL)

	// 源注册表
	registry := source.NewRegistry()
	registry.Register(&source.GitHubReleaseSource{})
	registry.Register(&source.GitHubTagSource{})
	registry.Register(&source.AURSource{})
	registry.Register(&source.ArchOfficialSource{})

	checker := &check.Checker{
		Registry:  registry,
		Store:     st,
		Threshold: 3,
		Timeout:   cfg.Sources.HTTPTimeout,
		UserAgent: cfg.Sources.UserAgent,
	}
	sched := &scheduler.Scheduler{
		Store:   st,
		Checker: checker,
		Tick:    cfg.Scheduler.Tick,
		MaxConc: cfg.Scheduler.MaxConc,
		Hosts:   scheduler.NewHostLimiter(cfg.Scheduler.HostRateLimit.DefaultPerMinute, cfg.Scheduler.HostRateLimit.Overrides),
	}

	// 通知编排
	notifiers := notifier.NewRegistry()
	notifiers.Register(&notifier.WebhookNotifier{})
	notifiers.Register(&notifier.TelegramNotifier{})
	dispatcher := &notify.Dispatcher{
		Store:       st,
		Notifiers:   notifiers,
		MaxAttempts: cfg.Notifications.Retry.MaxAttempts,
		BaseDelay:   cfg.Notifications.Retry.BaseDelay,
		MaxDelay:    cfg.Notifications.Retry.MaxDelay,
	}
	checker.OnChange = dispatcher.OnChange
	worker := &notify.Worker{
		Store:     st,
		Notifiers: notifiers,
	}

	// 渠道测试发送：直接用默认模板发一条
	testSender := func(ctx context.Context, ch *store.Channel) error {
		n, ok := notifiers.Get(ch.Type)
		if !ok {
			return fmt.Errorf("未知渠道类型 %s", ch.Type)
		}
		var cfgMap map[string]any
		_ = json.Unmarshal([]byte(ch.Config), &cfgMap)
		msg := &notifier.Message{
			Title:   "RelayWatch 测试消息",
			Body:    "这是一条来自 RelayWatch 的测试通知，说明该渠道配置正确。",
			Markdown: true,
		}
		return n.Send(ctx, cfgMap, msg)
	}
	// 手动重发：worker 同步处理一条 pending 记录
	resendOne := func(ctx context.Context, d *store.Delivery) error {
		worker.ProcessForced(ctx, d)
		return nil
	}

	srv := &httpapi.Server{
		Auth:      authSvc,
		OIDC:      oidcSvc,
		Store:     st,
		WebDist:   web.Dist(),
		Checker:   checker,
		Registry:  registry,
		Notifiers: notifiers,
		TestSender: testSender,
		ResendOne:  resendOne,
	}

	httpServer := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	hasUsers, err := authSvc.HasUsers(ctx)
	if err != nil {
		fatal(err)
	}
	slog.Info("RelayWatch 启动", "addr", cfg.Server.Addr, "release", release,
		"setup_required", !hasUsers, "db", cfg.Database.Path)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fatal(err)
		}
	}()

	sched.Start(ctx)
	worker.Start(ctx)
	slog.Info("调度器已启动", "tick", cfg.Scheduler.Tick, "max_conc", cfg.Scheduler.MaxConc)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("正在关闭…")
	sched.Stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

func fatal(err error) {
	slog.Error(err.Error())
	os.Exit(1)
}
