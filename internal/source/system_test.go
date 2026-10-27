package source

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestManualSource(t *testing.T) {
	s := &ManualSource{}
	ctx := context.Background()

	res, err := s.Fetch(ctx, map[string]any{
		"version": "1.0.4",
		"url":     "https://example.com/proj",
	})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "1.0.4" {
		t.Errorf("got %q, want 1.0.4", res.Version)
	}
	if res.URL != "https://example.com/proj" {
		t.Errorf("got url %q", res.URL)
	}
}

func TestCmdSource(t *testing.T) {
	ctx := context.Background()
	s := &CmdSource{}

	// 1. 默认禁用测试
	EnableCmdSource = false
	_, err := s.Fetch(ctx, map[string]any{"cmd": "echo 1.2.3"})
	if err == nil || !strings.Contains(err.Error(), "未在服务端启用") {
		t.Fatalf("expected security disabled error, got: %v", err)
	}

	// 2. 启用测试（带 mock runner）
	EnableCmdSource = true
	defer func() { EnableCmdSource = false }()

	oldRunner := cmdRunner
	defer func() { cmdRunner = oldRunner }()

	cmdRunner = func(ctx context.Context, cmdStr string, timeout time.Duration) (string, error) {
		return "v2.0.1\nsome extra debug info\n", nil
	}

	res, err := s.Fetch(ctx, map[string]any{"cmd": "echo version"})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Version != "v2.0.1" {
		t.Errorf("got %q, want v2.0.1", res.Version)
	}
}
