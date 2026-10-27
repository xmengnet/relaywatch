package notifier

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// DefaultTitle 默认标题模板。
const DefaultTitle = `RelayWatch：{{ .Monitor.Name }} 有新版本`

// DefaultCompareTitle 默认双轨对比标题模板。
const DefaultCompareTitle = `{{ if eq .Compare.Event "caught_up" }}RelayWatch：{{ .Monitor.Name }} 已追平上游{{ else }}RelayWatch：{{ .Monitor.Name }} 落后于上游{{ end }}`

// DefaultBody 默认正文模板（text/template，可用字段见 TemplateData）。
const DefaultBody = `{{ .Monitor.Name }}（{{ .Monitor.SourceType }}）发现新版本：
{{ if .Change.OldVersion }}{{ .Change.OldVersion }} → {{ end }}{{ .Change.NewVersion }}
{{ if .URL }}{{ .URL }}{{ end }}
发现时间：{{ .Change.DetectedAt.Format "2006-01-02 15:04:05" }}`

// DefaultCompareBody 默认双轨对比正文模板。
const DefaultCompareBody = `{{ if eq .Compare.Event "caught_up" }}{{ .Monitor.Name }}（{{ .Compare.FollowerVersion }}）已追平上游 {{ .Compare.RefMonitorName }}（{{ .Compare.RefVersion }}）{{ else }}{{ .Monitor.Name }}（{{ .Compare.FollowerVersion }}）当前落后于上游 {{ .Compare.RefMonitorName }}（{{ .Compare.RefVersion }}）{{ end }}`

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

// RenderCompare 渲染双轨对比通知消息。
func RenderCompare(customTitle, customBody string, data TemplateData, markdown bool) (title, body string, err error) {
	title, err = renderOne(customTitle, DefaultCompareTitle, data)
	if err != nil {
		if data.Compare.Event == "caught_up" {
			title = fmt.Sprintf("RelayWatch：%s 已追平上游", data.Monitor.Name)
		} else {
			title = fmt.Sprintf("RelayWatch：%s 落后于上游", data.Monitor.Name)
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
	t, err := template.New("msg").Funcs(template.FuncMap{
		"upper": strings.ToUpper,
	}).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("模板解析失败: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("模板执行失败: %w", err)
	}
	return strings.TrimSpace(buf.String()), nil
}
