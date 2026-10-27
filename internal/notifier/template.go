package notifier

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// DefaultTitle 默认标题模板。
const DefaultTitle = `RelayWatch：{{ .Monitor.Name }} 有新版本`

// DefaultBody 默认正文模板（text/template，可用字段见 TemplateData）。
const DefaultBody = `{{ .Monitor.Name }}（{{ .Monitor.SourceType }}）发现新版本：
{{ if .Change.OldVersion }}{{ .Change.OldVersion }} → {{ end }}{{ .Change.NewVersion }}
{{ if .URL }}{{ .URL }}{{ end }}
发现时间：{{ .Change.DetectedAt.Format "2006-01-02 15:04:05" }}`

// TemplateData 是模板可访问的数据。
type TemplateData struct {
	Monitor MonitorInfo
	Change  ChangeInfo
	URL     string
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
