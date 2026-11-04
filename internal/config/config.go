package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是 relaywatch.yaml 的完整结构。
type Config struct {
	Server struct {
		Addr       string `yaml:"addr"`
		BaseURL    string `yaml:"base_url"`
		TrustProxy bool   `yaml:"trust_proxy"` // 信任反向代理的 X-Forwarded-For / X-Real-IP
	} `yaml:"server"`

	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`

	Security struct {
		SecretKey      string        `yaml:"secret_key"`
		OldSecretKey   string        `yaml:"old_secret_key"`
		SessionTTL     time.Duration `yaml:"session_ttl"`
		LoginRateLimit int           `yaml:"login_rate_limit"`
		LoginLockout   time.Duration `yaml:"login_lockout"`
	} `yaml:"security"`

	Scheduler struct {
		Tick          time.Duration `yaml:"tick"`
		MaxConc       int           `yaml:"max_conc"`
		MinInterval   int           `yaml:"min_interval"` // 秒
		Jitter        float64       `yaml:"jitter"`       // 目前固定 0.1（确定性），预留配置
		HostRateLimit struct {
			DefaultPerMinute int            `yaml:"default_per_minute"`
			Overrides        map[string]int `yaml:"overrides"`
		} `yaml:"host_rate_limit"`
	} `yaml:"scheduler"`

	Sources struct {
		HTTPTimeout time.Duration `yaml:"http_timeout"`
		UserAgent   string        `yaml:"user_agent"`
	} `yaml:"sources"`

	Notifications struct {
		Retry struct {
			MaxAttempts int           `yaml:"max_attempts"`
			BaseDelay   time.Duration `yaml:"base_delay"`
			MaxDelay    time.Duration `yaml:"max_delay"`
		} `yaml:"retry"`
	} `yaml:"notifications"`

	Logging struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"logging"`

	Metrics struct {
		Enabled  bool   `yaml:"enabled"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"metrics"`
}

// Default 返回带合理默认值的配置。
func Default() *Config {
	c := &Config{}
	c.Server.Addr = ":8080"
	c.Server.BaseURL = "http://localhost:8080"
	c.Database.Path = "./data/relaywatch.db"
	c.Security.SessionTTL = 168 * time.Hour
	c.Security.LoginRateLimit = 5
	c.Security.LoginLockout = 15 * time.Minute
	c.Scheduler.Tick = 10 * time.Second
	c.Scheduler.MaxConc = 10
	c.Scheduler.MinInterval = 60
	c.Scheduler.HostRateLimit.DefaultPerMinute = 30
	c.Sources.HTTPTimeout = 20 * time.Second
	c.Sources.UserAgent = "RelayWatch/0.1"
	c.Notifications.Retry.MaxAttempts = 5
	c.Notifications.Retry.BaseDelay = 2 * time.Second
	c.Notifications.Retry.MaxDelay = 10 * time.Minute
	c.Logging.Level = "info"
	c.Logging.Format = "text"
	c.Metrics.Enabled = true
	return c
}

// Load 从 path 读取 YAML 配置，缺失字段保持默认值。
func Load(path string) (*Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	// 环境变量覆盖
	if v := os.Getenv("RELAYWATCH_ADDR"); v != "" {
		c.Server.Addr = v
	}
	if v := os.Getenv("RELAYWATCH_SECRET_KEY"); v != "" {
		c.Security.SecretKey = v
	}
	if v := os.Getenv("RELAYWATCH_METRICS_USERNAME"); v != "" {
		c.Metrics.Username = v
	}
	if v := os.Getenv("RELAYWATCH_METRICS_PASSWORD"); v != "" {
		c.Metrics.Password = v
	}
	return c, nil
}
