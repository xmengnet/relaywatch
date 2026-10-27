package main

import (
	"context"
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
	"relaywatch/internal/compare"
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

	// 源注册表（新增源类型在 internal/source/defaults.go 维护）
	registry := source.NewDefaultRegistry()

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

	// 通知渠道注册表（新增渠道在 internal/notifier/defaults.go 维护）
	notifiers := notifier.NewDefaultRegistry()
	dispatcher := &notify.Dispatcher{
		Store:       st,
		Notifiers:   notifiers,
		MaxAttempts: cfg.Notifications.Retry.MaxAttempts,
		BaseDelay:   cfg.Notifications.Retry.BaseDelay,
		MaxDelay:    cfg.Notifications.Retry.MaxDelay,
	}
		broker := httpapi.NewEventBroker()
		checker.OnEvent = func(ev check.Event) {
			broker.Broadcast("check.done", ev)
		}

		evaluator := compare.NewEvaluator(st, dispatcher.EnqueueCompare)
		checker.OnChange = dispatcher.OnChange
		checker.OnChecked = evaluator.EvaluateOnChecked
		worker := &notify.Worker{
			Store:     st,
			Notifiers: notifiers,
			OnDelivery: func(status store.DeliveryStatus) {
				broker.Broadcast("delivery.done", map[string]any{"status": status})
			},
		}
		dispatcher.OnDelivery = worker.OnDelivery

		// 渠道测试发送 / 手动重发
		testSender := notify.NewTestSender(notifiers)
		resendOne := func(ctx context.Context, d *store.Delivery) error {
			worker.ProcessForced(ctx, d)
			return nil
		}

		srv := &httpapi.Server{
			Auth:       authSvc,
			OIDC:       oidcSvc,
			Store:      st,
			WebDist:    web.Dist(), // dev 构建标签下为 nil（前端走 Vite :5173）
			Checker:    checker,
			Registry:   registry,
			Notifiers:  notifiers,
			Evaluator:  evaluator,
			Broker:     broker,
			TestSender: testSender,
			ResendOne:  resendOne,
		}
	if srv.WebDist == nil {
		slog.Info("dev 构建：静态资源未嵌入，前端请访问 Vite dev server (:5173)")
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
	// 先关 HTTP（排空在途请求），再停调度器和投递 worker（等在跑轮次落库）。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	sched.Stop()
	worker.Stop()
}

func fatal(err error) {
	slog.Error(err.Error())
	os.Exit(1)
}
