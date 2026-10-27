export type MonitorState = 'unknown' | 'up_to_date' | 'has_update' | 'error' | 'missing'

export interface User {
  id: number
  username: string
  email?: string
  role: 'admin' | 'viewer'
}

export interface Monitor {
  id: number
  name: string
  source_type: string
  // 旧版后端返回 JSON 字符串，现直出对象；消费方需兼容两者
  source_config: Record<string, unknown> | string
  version_prefix?: string
  version_suffix?: string
  version_include_regex?: string
  version_ignore_regex?: string
  interval_sec: number
  enabled: boolean
  state: MonitorState
  current_version?: string
  latest_version?: string
  last_checked_at?: string
  last_success_at?: string
  last_change_at?: string
  last_error?: string
  notify_cooldown_sec: number
  tags: string[]
  note?: string
  channel_ids?: number[]
  created_at: string
  updated_at: string
}

export interface SourceMeta {
  type: string
  label: string
  icon?: string
  fields: FieldSpec[]
}

export interface FieldSpec {
  key: string
  label: string
  type: 'string' | 'bool' | 'int' | 'select' | 'secret'
  required: boolean
  default?: unknown
  options?: string[]
  placeholder?: string
  help?: string
  pattern?: string
}
