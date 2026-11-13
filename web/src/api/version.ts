import { apiFetch } from './client'

export interface VersionInfo {
  version: string
}

export function getVersion() {
  return apiFetch<VersionInfo>('/version')
}
