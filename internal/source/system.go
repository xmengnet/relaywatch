package source

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// EnableCmdSource 控制服务端是否允许使用 cmd 监控源。
// 默认为 false（Web/多用户环境下防 RCE）；可通过环境变量 RELAYWATCH_ENABLE_CMD_SOURCE=1 显式开启。
var EnableCmdSource = os.Getenv("RELAYWATCH_ENABLE_CMD_SOURCE") == "1"

// cmdRunner 声明执行本地 shell 脚本的底层函数签名，单元测试中可直接 mock。
var cmdRunner = func(ctx context.Context, cmdStr string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	// 隔离环境，防止泄露 RELAYWATCH_SECRET_KEY 等敏感变量
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"LANG=C.UTF-8",
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = io.MultiWriter(&stdout)
	cmd.Stderr = io.MultiWriter(&stderr)

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("命令执行超时 (限时 %v)", timeout)
		}
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("命令执行失败: %s", errMsg)
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("命令执行成功但输出为空")
	}
	return out, nil
}

// ---------- Manual ----------

// ManualSource manual 源：手动固定版本，用于挂起人工确认或静态基线。
type ManualSource struct{}

func (s *ManualSource) Type() string { return "manual" }

func (s *ManualSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "手动固定版本",
		Icon:  "mdi-hand-back-right",
		Fields: []FieldSpec{
			{Key: "version", Label: "版本号", Type: "string", Required: true, Placeholder: "1.0.0"},
			{Key: "url", Label: "参考链接", Type: "string", Placeholder: "https://example.com/project"},
		},
	}
}

func (s *ManualSource) Validate(cfg map[string]any) error {
	return required(cfg, "version")
}

func (s *ManualSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	ver := cfgString(cfg, "version")
	if ver == "" {
		return nil, &NotFoundError{Msg: "未配置版本号"}
	}
	return &Result{
		Version: ver,
		URL:     cfgString(cfg, "url"),
	}, nil
}

// ---------- Cmd (受控) ----------

// CmdSource cmd 源：执行本地 shell 脚本提取版本（严格受控）。
type CmdSource struct{}

func (s *CmdSource) Type() string { return "cmd" }

func (s *CmdSource) Meta() Meta {
	return Meta{
		Type:  s.Type(),
		Label: "本地命令 (受控)",
		Icon:  "mdi-console-line",
		Fields: []FieldSpec{
			{Key: "cmd", Label: "Shell 命令", Type: "string", Required: true, Placeholder: "curl -s https://example.com/ver.txt", Help: "执行并提取首行标准输出"},
			{Key: "timeout", Label: "超时秒数", Type: "int", Default: 15, Help: "默认 15 秒"},
		},
	}
}

func (s *CmdSource) Validate(cfg map[string]any) error {
	return required(cfg, "cmd")
}

func (s *CmdSource) Fetch(ctx context.Context, cfg map[string]any) (*Result, error) {
	if !EnableCmdSource {
		return nil, fmt.Errorf("出于安全考量，cmd 监控源当前未在服务端启用（需管理员配置环境变量 RELAYWATCH_ENABLE_CMD_SOURCE=1）")
	}

	command := cfgString(cfg, "cmd")
	timeoutSec := atoiSafe(cfgString(cfg, "timeout"), 15)
	if timeoutSec <= 0 || timeoutSec > 120 {
		timeoutSec = 15
	}

	out, err := cmdRunner(ctx, command, time.Duration(timeoutSec)*time.Second)
	if err != nil {
		return nil, err
	}

	// 只取首行
	lines := strings.Split(out, "\n")
	firstLine := strings.TrimSpace(lines[0])
	if firstLine == "" {
		return nil, fmt.Errorf("命令输出首行为空")
	}

	return &Result{
		Version: firstLine,
	}, nil
}
