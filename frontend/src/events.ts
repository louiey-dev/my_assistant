import type { Camera, Device, Reading } from './api'
export type LiveEvent = { type: string; timestamp?: string; data?: Record<string, unknown> }
export type LiveStatus = 'connecting' | 'connected' | 'unavailable'

// Keep the dashboard current across Wi-Fi roaming, router idle timeouts, and
// laptops waking from sleep. A WebSocket does not reconnect by itself.
export function connectLiveEvents(onEvent: (event: LiveEvent) => void, filters: { devices: string[]; cameras: string[] }, onStatus: (status: LiveStatus, reconnected: boolean) => void) {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  let active = true
  let socket: WebSocket | null = null
  let retryTimer: number | null = null
  let retryDelay = 1000
  let hasConnected = false

  const scheduleReconnect = () => {
    if (!active || retryTimer !== null) return
    const delay = retryDelay
    retryDelay = Math.min(retryDelay * 2, 30000)
    retryTimer = window.setTimeout(() => {
      retryTimer = null
      open()
    }, delay)
  }
  const open = () => {
    if (!active) return
    onStatus('connecting', hasConnected)
    const next = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws`)
    socket = next
    next.addEventListener('open', () => {
      if (!active || socket !== next) return
      const reconnected = hasConnected
      hasConnected = true
      retryDelay = 1000
      onStatus('connected', reconnected)
      next.send(JSON.stringify({ type: 'subscribe', ...filters }))
    })
    next.addEventListener('message', (message) => { try { onEvent(JSON.parse(message.data) as LiveEvent) } catch { /* Ignore malformed events. */ } })
    next.addEventListener('error', () => onStatus('unavailable', hasConnected))
    next.addEventListener('close', (event) => {
      if (!active || socket !== next) return
      onStatus('unavailable', hasConnected)
      // This is intentionally visible in DevTools; it provides the browser
      // close code/reason needed to diagnose a future network interruption.
      console.warn('Live connection closed; reconnecting.', { code: event.code, reason: event.reason || 'none', wasClean: event.wasClean })
      scheduleReconnect()
    })
  }
  open()
  return () => {
    active = false
    if (retryTimer !== null) window.clearTimeout(retryTimer)
    socket?.close()
  }
}
export function eventDevice(event: LiveEvent): Device | null { return event.data as Device | null }
export function eventReading(event: LiveEvent): { deviceId: string; reading: Reading } | null { const data = event.data; if (!data) return null; const deviceId = String(data.device_id ?? data.deviceId ?? ''); return deviceId ? { deviceId, reading: data as Reading } : null }
export function eventCamera(event: LiveEvent): Camera | null { return event.data as Camera | null }
