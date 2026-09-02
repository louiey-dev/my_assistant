import type { Camera, Device, Reading } from './api'
export type LiveEvent = { type: string; timestamp?: string; data?: Record<string, unknown> }
export function connectLiveEvents(onEvent: (event: LiveEvent) => void, filters: { devices: string[]; cameras: string[] }) {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws`)
  socket.addEventListener('open', () => socket.send(JSON.stringify({ type: 'subscribe', ...filters })))
  socket.addEventListener('message', (message) => { try { onEvent(JSON.parse(message.data) as LiveEvent) } catch { /* Ignore malformed events. */ } })
  return socket
}
export function eventDevice(event: LiveEvent): Device | null { return event.data as Device | null }
export function eventReading(event: LiveEvent): { deviceId: string; reading: Reading } | null { const data = event.data; if (!data) return null; const deviceId = String(data.device_id ?? data.deviceId ?? ''); return deviceId ? { deviceId, reading: data as Reading } : null }
export function eventCamera(event: LiveEvent): Camera | null { return event.data as Camera | null }
