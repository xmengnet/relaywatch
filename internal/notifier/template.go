package notifier

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// DefaultTitle 默认标题模板。
const DefaultTitle = `RelayWatch：{{ .Monitor.Name }} 有新版本`

// DefaultCompareTitle 默认双轨对比标题模板（带上游名，一眼看出谁跟谁比）。
const DefaultCompareTitle = `{{ if eq .Compare.Event "caught_up" }}RelayWatch：{{ .Monitor.Name }} 已追平上游 {{ .Compare.RefMonitorName }}{{ else }}RelayWatch：{{ .Monitor.Name }} 落后于上游 {{ .Compare.RefMonitorName }}{{ end }}`

// DefaultBody 默认正文模板（text/template，可用字段见 TemplateData）。
const DefaultBody = `🚀 {{ .Monitor.Name }}（{{ .Monitor.SourceType }}）发现新版本
{{ if .Change.OldVersion }}版本：{{ .Change.OldVersion }} → {{ .Change.NewVersion }}{{ else }}版本：{{ .Change.NewVersion }}（新监控基线）{{ end }}
{{ if .Monitor.Tags }}标签：{{ .Monitor.Tags | joinTags }}{{ end }}
{{ if .URL }}链接：{{ .URL }}{{ end }}
时间：{{ .Change.DetectedAt.Format "2006-01-02 15:04:05" }}
{{ if .Change.OldVersion }}确认更新后停止提醒，可到监控详情页操作。{{ else }}首次发现版本，已记为待确认。{{ end }}`

// DefaultCompareBody 默认双轨对比正文模板。
// 三种事件（entered_lagging / gap_grew / caught_up）分别渲染，带两侧版本与版本线。
const DefaultCompareBody = `{{ if eq .Compare.Event "caught_up" }}✅ {{ .Monitor.Name }} 已追平上游
{{ else if eq .Compare.Event "gap_grew" }}📉 {{ .Monitor.Name }} 落后幅度扩大（上游又发布了新版本）
{{ else }}⚠️ {{ .Monitor.Name }} 落后于上游{{ end }}
本包：{{ .Monitor.Name }}（{{ .Monitor.SourceType }}）{{ .Compare.FollowerVersion }}
上游：{{ .Compare.RefMonitorName }} {{ .Compare.RefVersion }}
{{ if ne .Compare.RefVersion .Compare.FollowerVersion }}差距：{{ .Compare.FollowerVersion }} ⇐ {{ .Compare.RefVersion }}{{ else }}两侧版本一致{{ end }}
{{ if .URL }}上游链接：{{ .URL }}{{ end }}
状态：{{ .Compare.State | compareStateText }}（{{ .Compare.Event }}）
时间：{{ .Change.DetectedAt.Format "2006-01-02 15:04:05" }}`

// CompareInfo 双轨对比相关信息。
type CompareInfo struct {
	RefMonitorName  string
	RefVersion      string
	FollowerVersion string
	State           string // lagging | in_sync | ahead
	Event           string // entered_lagging | gap_grew | caught_up
}

// TemplateData 是模板可访问的数据。
type TemplateData struct {
	Monitor MonitorInfo
	Change  ChangeInfo
	Compare CompareInfo
	URL     string
}

var templateFuncs = template.FuncMap{
	"upper": strings.ToUpper,
	// joinTags 把标签切片渲染为逗号分隔（空切片输出空串）
	"joinTags": func(tags []string) string {
		return strings.Join(tags, ", ")
	},
	// compareStateText 把对比状态翻译为可读文案
	"compareStateText": func(state string) string {
		switch state {
		case "lagging":
			return "落后"
		case "in_sync":
			return "已同步"
		case "ahead":
			return "领先"
		}
		return state
	},
}

// RenderCompare 渲染双轨对比通知消息。
func RenderCompare(customTitle, customBody string, data TemplateData, markdown bool) (title, body string, err error) {
	title, err = renderOne(customTitle, DefaultCompareTitle, data)
	if err != nil {
		if data.Compare.Event == "caught_up" {
			title = fmt.Sprintf("RelayWatch：%s 已追平上游 %s", data.Monitor.Name, data.Compare.RefMonitorName)
		} else {
			title = fmt.Sprintf("RelayWatch：%s 落后于上游 %s", data.Monitor.Name, data.Compare.RefMonitorName)
		}
	}
	body, err = renderOne(customBody, DefaultCompareBody, data)
	if err != nil {
		return title, "", err
	}
	return title, body, nil
}

// Render 用自定义或默认模板渲染标题与正文。
// custom 为空则用内置默认。模板出错时回退默认并返回错误信息。
func Render(customTitle, customBody string, data TemplateData, markdown bool) (title, body string, err error) {
	title, err = renderOne(customTitle, DefaultTitle, data)
	if err != nil {
		title = fmt.Sprintf("RelayWatch：%s 有新版本", data.Monitor.Name)
	}
	body, err = renderOne(customBody, DefaultBody, data)
	if err != nil {
		return title, "", err
	}
	return title, body, nil
}

func renderOne(custom, fallback string, data TemplateData) (string, error) {
	tmplStr := strings.TrimSpace(custom)
	if tmplStr == "" {
		tmplStr = fallback
	}
	// 自定义模板：text/template（需求 3.3）；默认模板同语法
	t, err := template.New("msg").Funcs(templateFuncs).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("模板解析失败: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("模板执行失败: %w", err)
	}
	return strings.TrimSpace(buf.String()), nil
}
