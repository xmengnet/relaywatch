import { i18n } from '@/i18n'

let csrfToken = ''

const httpStatusGatewayTimeout = 504

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public fields?: Record<string, string>,
    public code?: string,
  ) {
    super(message)
  }
}

// 常规 API 请求 30 秒超时；SSE 走 EventSource 不经过这里，不受影响。
const REQUEST_TIMEOUT_MS = 30_000

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

  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)

  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      ...init,
      headers,
      credentials: 'same-origin',
      signal: controller.signal,
    })
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') {
      throw new ApiError(i18n.global.t('common.timeout'), httpStatusGatewayTimeout, undefined, 'timeout')
    }
    throw e
  } finally {
    clearTimeout(timer)
  }

  if (res.status === 401) {
    // 已在登录页时不再整页跳转，否则探测性请求的 401 会造成无限刷新
    if (!opts.skipAuthRedirect && location.pathname !== '/login') {
      location.href = '/login'
    }
    const body = await res.json().catch(() => ({}))
    throw new ApiError(body.message ?? i18n.global.t('common.unauthorized'), 401, body.fields, body.code)
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    // 服务端 500 只返回 code=internal，具体原因在日志里；这里换成本地化文案
    const message = body.code === 'internal'
      ? i18n.global.t('common.internalError')
      : (body.message ?? `HTTP ${res.status}`)
    throw new ApiError(message, res.status, body.fields, body.code)
  }

  return res.status === 204 ? (undefined as T) : res.json()
}

export function setCsrf(token: string) {
  csrfToken = token
}

export function getCsrf() {
  return csrfToken
}
