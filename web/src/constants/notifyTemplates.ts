import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

// 通知模板助手的共享常量：渠道编辑弹窗（ChannelListView）与帮助中心（HelpView）共用。
// 变量说明与预设名称/描述走 i18n；模板正文按语言各存一份，避免把 Go 模板放进
// vue-i18n 消息（{{ }} 会被当成插值解析）。

export interface TemplateVariableField {
  key: string
  desc: string
}

export interface TemplateVariableGroup {
  group: string
  fields: TemplateVariableField[]
}

interface VariableDef {
  key: string
  descKey: string
}

const variableGroupDefs: Array<{ groupKey: string; fields: VariableDef[] }> = [
  {
    groupKey: 'monitor',
    fields: [
      { key: '{{ .Monitor.Name }}', descKey: 'monitorName' },
      { key: '{{ .Monitor.SourceType }}', descKey: 'sourceType' },
      { key: '{{ .Monitor.Tags }}', descKey: 'tags' },
    ],
  },
  {
    groupKey: 'change',
    fields: [
      { key: '{{ .Change.NewVersion }}', descKey: 'newVersion' },
      { key: '{{ .Change.OldVersion }}', descKey: 'oldVersion' },
      { key: '{{ .Change.DetectedAt.Format "2006-01-02 15:04:05" }}', descKey: 'detectedAt' },
      { key: '{{ .URL }}', descKey: 'url' },
    ],
  },
  {
    groupKey: 'compare',
    fields: [
      { key: '{{ .Compare.RefMonitorName }}', descKey: 'refMonitorName' },
      { key: '{{ .Compare.RefVersion }}', descKey: 'refVersion' },
      { key: '{{ .Compare.FollowerVersion }}', descKey: 'followerVersion' },
      { key: '{{ .Compare.State }}', descKey: 'compareState' },
      { key: '{{ .Compare.Event }}', descKey: 'compareEvent' },
    ],
  },
]

export type TemplatePresetKey = 'standard' | 'compact' | 'markdown' | 'compare'

export interface TemplatePreset {
  key: TemplatePresetKey
  name: string
  desc: string
  template: string
}

const presetKeys: TemplatePresetKey[] = ['standard', 'compact', 'markdown', 'compare']

const presetTemplates: Record<'zh-Hans' | 'en', Record<TemplatePresetKey, string>> = {
  'zh-Hans': {
    standard: `{{ .Monitor.Name }}（{{ .Monitor.SourceType }}）发现新版本：
{{ if .Change.OldVersion }}{{ .Change.OldVersion }} → {{ end }}{{ .Change.NewVersion }}
{{ if .URL }}{{ .URL }}{{ end }}
发现时间：{{ .Change.DetectedAt.Format "2006-01-02 15:04:05" }}`,
    compact: `【{{ .Monitor.Name }}】{{ if .Change.OldVersion }}{{ .Change.OldVersion }} ➔ {{ end }}{{ .Change.NewVersion }} {{ if .URL }}({{ .URL }}){{ end }}`,
    markdown: `### 🚀 {{ .Monitor.Name }} 发布新版本
- **最新版本**：\`{{ .Change.NewVersion }}\`
{{ if .Change.OldVersion }}- **上个版本**：\`{{ .Change.OldVersion }}\`{{ end }}
{{ if .URL }}- **发布页面**：[{{ .Monitor.Name }}]({{ .URL }}){{ end }}
> 监测时间：{{ .Change.DetectedAt.Format "2006-01-02 15:04:05" }}`,
    compare: `{{ if eq .Compare.Event "caught_up" }}🎉 {{ .Monitor.Name }}（{{ .Compare.FollowerVersion }}）已成功追平上游 {{ .Compare.RefMonitorName }}！{{ else }}⚠️ {{ .Monitor.Name }}（{{ .Compare.FollowerVersion }}）当前落后于上游 {{ .Compare.RefMonitorName }}（{{ .Compare.RefVersion }}）{{ end }}
{{ if .URL }}参考链接：{{ .URL }}{{ end }}`,
  },
  en: {
    standard: `{{ .Monitor.Name }} ({{ .Monitor.SourceType }}) has a new version:
{{ if .Change.OldVersion }}{{ .Change.OldVersion }} → {{ end }}{{ .Change.NewVersion }}
{{ if .URL }}{{ .URL }}{{ end }}
Detected: {{ .Change.DetectedAt.Format "2006-01-02 15:04:05" }}`,
    compact: `[{{ .Monitor.Name }}] {{ if .Change.OldVersion }}{{ .Change.OldVersion }} ➔ {{ end }}{{ .Change.NewVersion }} {{ if .URL }}({{ .URL }}){{ end }}`,
    markdown: `### 🚀 {{ .Monitor.Name }} released a new version
- **Latest version**: \`{{ .Change.NewVersion }}\`
{{ if .Change.OldVersion }}- **Previous version**: \`{{ .Change.OldVersion }}\`{{ end }}
{{ if .URL }}- **Release page**: [{{ .Monitor.Name }}]({{ .URL }}){{ end }}
> Detected: {{ .Change.DetectedAt.Format "2006-01-02 15:04:05" }}`,
    compare: `{{ if eq .Compare.Event "caught_up" }}🎉 {{ .Monitor.Name }} ({{ .Compare.FollowerVersion }}) has caught up with upstream {{ .Compare.RefMonitorName }}!{{ else }}⚠️ {{ .Monitor.Name }} ({{ .Compare.FollowerVersion }}) is behind upstream {{ .Compare.RefMonitorName }} ({{ .Compare.RefVersion }}){{ end }}
{{ if .URL }}Reference: {{ .URL }}{{ end }}`,
  },
}

export function useTemplateVariables() {
  const { t } = useI18n()
  return computed<TemplateVariableGroup[]>(() =>
    variableGroupDefs.map(g => ({
      group: t(`templates.groups.${g.groupKey}`),
      fields: g.fields.map(f => ({
        key: f.key,
        desc: t(`templates.vars.${f.descKey}`),
      })),
    })),
  )
}

export function useTemplatePresets() {
  const { t, locale } = useI18n()
  return computed<TemplatePreset[]>(() => {
    const lang = locale.value === 'en' ? 'en' : 'zh-Hans'
    return presetKeys.map((key) => ({
      key,
      name: t(`templates.presets.${key}.name`),
      desc: t(`templates.presets.${key}.desc`),
      template: presetTemplates[lang][key],
    }))
  })
}
