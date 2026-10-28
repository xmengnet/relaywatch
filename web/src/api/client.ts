import { i18n } from '@/i18n'

let csrfToken = ''

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public fields?: Record<string, string>,
  ) {
    super(message)
  }
}

export async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
  opts: { skipAuthRedirect?: boolean } = {},
): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  if (init.method && init.method !== 'GET' && csrfToken) {
    headers.set('X-CSRF-Token', csrfToken)
  }

  const res = await fetch(`/api/v1${path}`, {
    ...init,
    headers,
    credentials: 'same-origin',
  })

  if (res.status === 401) {
    // 已在登录页时不再整页跳转，否则探测性请求的 401 会造成无限刷新
    if (!opts.skipAuthRedirect && location.pathname !== '/login') {
      location.href = '/login'
    }
    const body = await res.json().catch(() => ({}))
    throw new ApiError(body.message ?? i18n.global.t('common.unauthorized'), 401, body.fields)
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new ApiError(body.message ?? `HTTP ${res.status}`, res.status, body.fields)
  }

  return res.status === 204 ? (undefined as T) : res.json()
}

export function setCsrf(token: string) {
  csrfToken = token
}

export function getCsrf() {
  return csrfToken
}
