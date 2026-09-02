export type User = { user_id: number; username: string; role: string }
export type Health = { status: string }
export type SystemStatus = { backend?: string; broker?: string; database?: string; streaming?: string }
export type Reading = { value?: number | string; unit?: string; timestamp?: string; metric?: string }
export type Device = { device_id: string; name?: string; type?: string; available?: boolean; state?: string; latest_reading?: Reading }
export type Camera = { camera_id: string; name?: string; available?: boolean; state?: string; capabilities?: string[] }
export type CommandResult = { request_id: string; status: string }

export class ApiError extends Error {
  status: number
  code: string
  constructor(status: number, code: string, message: string) { super(message); this.name = 'ApiError'; this.status = status; this.code = code }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(path, { credentials: 'same-origin', headers: { Accept: 'application/json', ...(init.body ? { 'Content-Type': 'application/json' } : {}) }, ...init })
  if (response.status === 204) return undefined as T
  const body = await response.json().catch(() => null) as T & { error?: { code?: string; message?: string } }
  if (!response.ok) throw new ApiError(response.status, body?.error?.code ?? 'request_failed', body?.error?.message ?? `Request failed (${response.status})`)
  return body
}

export const api = {
  health: () => request<Health>('/healthz'),
  me: () => request<User>('/api/v1/auth/me'),
  login: (username: string, password: string) => request<User>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
  logout: () => request<void>('/api/v1/auth/logout', { method: 'POST' }),
  status: () => request<SystemStatus>('/api/v1/status'),
  devices: () => request<Device[]>('/api/v1/devices'),
  readings: (id: string) => request<Reading[]>(`/api/v1/devices/${encodeURIComponent(id)}/readings`),
  cameras: () => request<Camera[]>('/api/v1/cameras'),
  cameraStream: (id: string) => request<{ url?: string; type?: string }>(`/api/v1/cameras/${encodeURIComponent(id)}/stream`),
  command: (id: string, command: string, parameters: Record<string, unknown> = {}) => request<CommandResult>(`/api/v1/devices/${encodeURIComponent(id)}/commands`, { method: 'POST', body: JSON.stringify({ command, parameters }) }),
  cameraCommand: (id: string, command: string, parameters: Record<string, unknown> = {}) => request<CommandResult>(`/api/v1/cameras/${encodeURIComponent(id)}/commands`, { method: 'POST', body: JSON.stringify({ command, parameters }) }),
}
