import { onMounted, onUnmounted } from 'vue'

export type SseHandler = (data: any) => void

const handlers = new Map<string, Set<SseHandler>>()
let source: EventSource | null = null
let retryCount = 0
let reconnectTimer: number | null = null

function connect() {
  if (source || typeof window === 'undefined' || !window.EventSource) return

  source = new EventSource('/api/v1/events')

  source.onopen = () => {
    retryCount = 0
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
  }

  source.onerror = () => {
    source?.close()
    source = null
    const delay = Math.min(30000, 1000 * 2 ** retryCount++)
    if (!reconnectTimer) {
      reconnectTimer = window.setTimeout(() => {
        reconnectTimer = null
        connect()
      }, delay)
    }
  }

  // 挂载所有已注册事件监听
  for (const [eventName, set] of handlers) {
    source.addEventListener(eventName, (e: MessageEvent) => {
      let data = e.data
      try {
        data = JSON.parse(e.data)
      } catch {
        // 保留原值
      }
      for (const h of set) {
        try {
          h(data)
        } catch (err) {
          console.error(`[SSE] handler error for ${eventName}:`, err)
        }
      }
    })
  }
}

/**
 * useSse 订阅后端 SSE 实时事件。
 * 离开组件时自动注销回调，当所有组件注销后可选择关闭长连接。
 */
export function useSse(event: string, handler: SseHandler) {
  onMounted(() => {
    if (!handlers.has(event)) {
      handlers.set(event, new Set())
    }
    handlers.get(event)!.add(handler)

    if (!source) {
      connect()
    } else {
      // 若已连接，为新增的 event 单独附加监听
      source.addEventListener(event, (e: MessageEvent) => {
        let data = e.data
        try {
          data = JSON.parse(e.data)
        } catch {}
        handler(data)
      })
    }
  })

  onUnmounted(() => {
    const set = handlers.get(event)
    if (set) {
      set.delete(handler)
      if (set.size === 0) {
        handlers.delete(event)
      }
    }
    if (handlers.size === 0 && source) {
      source.close()
      source = null
    }
  })
}
