export type MonitorState = 'unknown' | 'up_to_date' | 'has_update' | 'error' | 'missing'

export type Role = 'admin' | 'user' | 'viewer'

export interface User {
  id: number
  username: string
  email?: string
  display_name?: string
  role: Role
}

export interface AdminUser extends User {
  permissions?: string[] | null
  oidc_provider?: string
  disabled: boolean
  created_at: string
  last_login_at?: string
  monitor_count: number
}

export interface Monitor {
  id: number
  owner_id?: number
  owner_name?: string
  name: string
  source_type: string
  // 旧版后端返回 JSON 字符串，现直出对象；消费方需兼容两者
  source_config: Record<string, unknown> | string
  version_prefix?: string
  version_suffix?: string
  version_include_regex?: string
  version_ignore_regex?: string
  from_pattern?: string
  to_pattern?: string
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
  reference_monitor_id?: number | null
  reference_monitor_name?: string
  compare_state?: string
  compare_ref_version?: string
  compare_since?: string
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
