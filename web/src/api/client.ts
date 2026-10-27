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

export async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
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
    location.href = '/login'
    throw new ApiError('未登录', 401)
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
