import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Alert, AppBar, Box, Button, Card, CardContent, Chip, CircularProgress, Container, Divider, Grid, IconButton, LinearProgress, Paper, Stack, TextField, Toolbar, Tooltip, Typography } from '@mui/material'
import { api, ApiError, Camera, Device, Health, Reading, SystemStatus, User } from './api'
import { connectLiveEvents, eventCamera, eventDevice, eventReading, LiveEvent } from './events'

type LoadState<T> = { value: T; loading: boolean; error: string | null }
const initialLoad = <T,>(value: T): LoadState<T> => ({ value, loading: true, error: null })
const messageFor = (error: unknown) => error instanceof ApiError ? error.message : 'The backend could not be reached.'

function Login({ onLogin }: { onLogin: (user: User) => void }) {
  const [username, setUsername] = useState(''); const [password, setPassword] = useState(''); const [error, setError] = useState<string | null>(null); const [loading, setLoading] = useState(false)
  const submit = async (event: FormEvent) => { event.preventDefault(); setLoading(true); setError(null); try { onLogin(await api.login(username, password)) } catch (reason) { setError(messageFor(reason)) } finally { setLoading(false) } }
  return <Container maxWidth="sm" sx={{ py: { xs: 5, md: 12 } }}><Card component="form" onSubmit={submit} elevation={3}><CardContent sx={{ p: { xs: 3, md: 5 } }}><Typography variant="h4" fontWeight={700} gutterBottom>Sign in</Typography><Typography color="text.secondary" sx={{ mb: 3 }}>Sign in to view and control your home monitoring system.</Typography>{error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}<Stack spacing={2}><TextField label="Username" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required autoFocus /><TextField label="Password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required /><Button type="submit" variant="contained" size="large" disabled={loading}>{loading ? <CircularProgress size={24} /> : 'Sign in'}</Button></Stack></CardContent></Card></Container>
}

function StatusChip({ online, label }: { online?: boolean; label?: string }) { return <Chip size="small" label={label ?? (online ? 'Online' : 'Offline')} color={online ? 'success' : 'default'} variant={online ? 'filled' : 'outlined'} /> }
function ErrorState({ message, retry }: { message: string; retry?: () => void }) { return <Alert severity="warning" action={retry && <Button color="inherit" size="small" onClick={retry}>Retry</Button>}>{message}</Alert> }

function SensorCard({ device, onCommand, onDetails }: { device: Device; onCommand: (device: Device) => void; onDetails: (device: Device) => void }) {
  const reading = device.latest_reading
  return <Card sx={{ height: '100%' }}><CardContent><Stack direction="row" justifyContent="space-between" alignItems="flex-start" gap={1}><Box><Typography variant="h6">{device.name || device.device_id}</Typography><Typography variant="caption" color="text.secondary">{device.type || 'Sensor'} · {device.device_id}</Typography></Box><StatusChip online={device.available} /></Stack><Typography variant="h3" sx={{ mt: 2 }}>{reading?.value ?? '—'} <Typography component="span" variant="body1" color="text.secondary">{reading?.unit}</Typography></Typography><Typography variant="caption" color="text.secondary">{reading?.timestamp ? new Date(reading.timestamp).toLocaleString() : 'No reading received'}</Typography><Stack direction="row" spacing={1} sx={{ mt: 2 }}><Button size="small" variant="outlined" onClick={() => onDetails(device)}>Details</Button><Button size="small" variant="outlined" disabled={!device.available} onClick={() => onCommand(device)}>Send command</Button></Stack></CardContent></Card>
}

function SensorDetails({ device, onClose }: { device: Device; onClose: () => void }) {
  const [readings, setReadings] = useState<Reading[]>([]); const [error, setError] = useState<string | null>(null)
  useEffect(() => { api.readings(device.device_id).then((value) => setReadings(value)).catch((reason) => setError(messageFor(reason))) }, [device.device_id])
  const points = readings.map((reading) => Number(reading.value)).filter(Number.isFinite); const min = Math.min(...points, 0); const max = Math.max(...points, 1); const polyline = points.map((value, index) => `${(index / Math.max(points.length - 1, 1)) * 280 + 10},${110 - ((value - min) / Math.max(max - min, 1)) * 90}`).join(' ')
  return <Paper elevation={6} sx={{ position: 'fixed', inset: 0, m: 'auto', width: 'min(560px, calc(100% - 32px))', height: 'fit-content', maxHeight: 'calc(100% - 32px)', overflow: 'auto', p: 3, zIndex: 10 }}><Stack direction="row" justifyContent="space-between" alignItems="center"><Box><Typography variant="h6">{device.name || device.device_id}</Typography><Typography color="text.secondary">Reading history</Typography></Box><Button onClick={onClose}>Close</Button></Stack>{error ? <Alert severity="info" sx={{ mt: 2 }}>{error}</Alert> : readings.length === 0 ? <Alert severity="info" sx={{ mt: 2 }}>No historical readings are available yet.</Alert> : <><Box component="svg" viewBox="0 0 300 120" sx={{ width: '100%', mt: 3, bgcolor: 'action.hover', borderRadius: 1 }}><polyline points={polyline} fill="none" stroke="currentColor" strokeWidth="3" /></Box><Typography variant="caption" color="text.secondary">{readings.length} readings · {readings[0].unit || ''}</Typography></>}</Paper>
}

function MjpegStream({ url, alt, onFailure }: { url: string; alt: string; onFailure: () => void }) {
  const [frame, setFrame] = useState<string | null>(null)
  useEffect(() => {
    const controller = new AbortController()
    let active = true
    let failed = false
    let frameURL: string | null = null
    let lastPaint = 0
    let noFrameTimer: number | null = null
    const fail = () => {
      if (!active || failed) return
      failed = true
      controller.abort()
      onFailure()
    }
    const armNoFrameTimer = () => {
      if (noFrameTimer !== null) window.clearTimeout(noFrameTimer)
      noFrameTimer = window.setTimeout(fail, 20000)
    }
    const marker = (bytes: Uint8Array, first: number, second: number, from: number) => {
      for (let index = from; index < bytes.length - 1; index += 1) if (bytes[index] === first && bytes[index + 1] === second) return index
      return -1
    }
    const displayFrame = (jpeg: Uint8Array) => {
      armNoFrameTimer()
      if (performance.now() - lastPaint < 100) return
      lastPaint = performance.now()
      const copy = new Uint8Array(jpeg.length)
      copy.set(jpeg)
      const nextURL = URL.createObjectURL(new Blob([copy.buffer], { type: 'image/jpeg' }))
      if (!active) return URL.revokeObjectURL(nextURL)
      const previousURL = frameURL
      frameURL = nextURL
      setFrame(nextURL)
      if (previousURL) URL.revokeObjectURL(previousURL)
    }
    const read = async () => {
      try {
        const response = await fetch(url, { credentials: 'same-origin', cache: 'no-store', signal: controller.signal })
        if (!response.ok || !response.body) throw new Error('Camera stream unavailable')
        armNoFrameTimer()
        const reader = response.body.getReader()
        let buffered = new Uint8Array(0)
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          const next = new Uint8Array(buffered.length + value.length)
          next.set(buffered)
          next.set(value, buffered.length)
          buffered = next
          for (;;) {
            const start = marker(buffered, 0xff, 0xd8, 0)
            if (start < 0) {
              buffered = buffered.slice(Math.max(buffered.length - 1, 0))
              break
            }
            const end = marker(buffered, 0xff, 0xd9, start + 2)
            if (end < 0) {
              buffered = buffered.slice(start)
              break
            }
            displayFrame(buffered.slice(start, end + 2))
            buffered = buffered.slice(end + 2)
          }
        }
        fail()
      } catch (reason) {
        if (active && !(reason instanceof DOMException && reason.name === 'AbortError')) fail()
      }
    }
    void read()
    return () => {
      active = false
      controller.abort()
      if (noFrameTimer !== null) window.clearTimeout(noFrameTimer)
      if (frameURL) URL.revokeObjectURL(frameURL)
    }
  }, [url, onFailure])
  return frame ? <Box component="img" src={frame} alt={alt} sx={{ width: '100%', height: '100%', objectFit: 'contain' }} /> : <Typography>Connecting to camera…</Typography>
}

function CameraCard({ camera, onCommand }: { camera: Camera; onCommand: (camera: Camera) => void }) {
  const [stream, setStream] = useState<{ url: string; type?: string } | null>(null); const [streamError, setStreamError] = useState<string | null>(null)
  const retryTimer = useRef<number | null>(null)
  const retryDelay = useRef(2000)
  const attempt = useRef(0)
  const loadSequence = useRef(0)
  const previousAvailability = useRef<boolean | undefined>(undefined)
  const loadStream = useCallback(async () => {
    const sequence = ++loadSequence.current
    setStreamError(null)
    try {
      const descriptor = await api.cameraStream(camera.camera_id)
      if (!descriptor.url) throw new Error('The camera did not provide a stream URL.')
      if (sequence !== loadSequence.current) return
      attempt.current += 1
      // Force a fresh browser/proxy connection after a failed MJPEG request.
      const separator = descriptor.url.includes('?') ? '&' : '?'
      setStream({ url: `${descriptor.url}${separator}attempt=${attempt.current}`, type: descriptor.type })
      retryDelay.current = 2000
    } catch (reason) {
      if (sequence !== loadSequence.current) return
      setStream(null)
      setStreamError(messageFor(reason))
      scheduleRetry()
    }
  }, [camera.camera_id])
  const scheduleRetry = useCallback(() => {
    if (retryTimer.current !== null) return
    const delay = retryDelay.current
    retryDelay.current = Math.min(delay * 2, 30000)
    retryTimer.current = window.setTimeout(() => { retryTimer.current = null; void loadStream() }, delay)
  }, [loadStream])
  const restartStream = useCallback(() => {
    // Unmount the old <img> before creating the replacement. This is needed
    // when the browser has not noticed that the previous MJPEG socket died.
    ++loadSequence.current
    if (retryTimer.current !== null) {
      window.clearTimeout(retryTimer.current)
      retryTimer.current = null
    }
    setStream(null)
    setStreamError(null)
    void loadStream()
  }, [loadStream])
  useEffect(() => {
    const wentOffline = camera.available === false && previousAvailability.current !== false
    const recovered = camera.available === true && previousAvailability.current === false
    const firstLoad = previousAvailability.current === undefined
    previousAvailability.current = camera.available
    if (wentOffline) {
      // Do not leave a half-open MJPEG element visible while the camera is
      // rebooting. It can prevent the browser from opening the recovered
      // stream and makes the card look online/usable when it is not.
      ++loadSequence.current
      setStream(null)
      setStreamError('Camera offline; waiting for it to reconnect…')
      if (retryTimer.current !== null) {
        window.clearTimeout(retryTimer.current)
        retryTimer.current = null
      }
    } else if (firstLoad || recovered) {
      // A camera can keep the old TCP/MJPEG connection half-open while Wi-Fi
      // is recovering. Replacing the image forces a new request immediately
      // when the monitor reports the camera online again.
      if (retryTimer.current !== null) {
        window.clearTimeout(retryTimer.current)
        retryTimer.current = null
      }
      if (camera.available === true) void restartStream()
    }
    return () => {
      if (retryTimer.current !== null) window.clearTimeout(retryTimer.current)
    }
  }, [camera.available, restartStream])
  const retryStream = useCallback(() => {
    // The browser is the first component that can observe a broken MJPEG
    // connection. Show the offline state immediately instead of waiting for
    // the backend's slower availability probe.
    ++loadSequence.current
    setStream(null)
    setStreamError('Camera offline; waiting for it to reconnect…')
    scheduleRetry()
  }, [scheduleRetry])
  return <Card sx={{ height: '100%' }}><Box sx={{ aspectRatio: '16 / 9', bgcolor: 'grey.900', display: 'grid', placeItems: 'center', color: 'grey.300', p: 2 }}>{stream ? stream.type === 'mjpeg' ? <MjpegStream key={stream.url} url={stream.url} onFailure={retryStream} alt={`${camera.name || camera.camera_id} stream`} /> : <Box key={stream.url} component="video" src={stream.url} onError={retryStream} onAbort={retryStream} controls sx={{ width: '100%', height: '100%' }} /> : <Stack alignItems="center" spacing={1}><Typography>{streamError || 'Stream placeholder'}</Typography><Button variant="contained" size="small" onClick={restartStream}>Load stream</Button></Stack>}</Box><CardContent><Stack direction="row" justifyContent="space-between" gap={1}><Box><Typography variant="h6">{camera.name || camera.camera_id}</Typography><Typography variant="caption" color="text.secondary">{camera.camera_id}</Typography></Box><StatusChip online={camera.available} /></Stack>{streamError && stream && <Alert severity="info" sx={{ mt: 1 }}>{streamError}</Alert>}<Button sx={{ mt: 1 }} size="small" variant="outlined" disabled={!camera.available} onClick={() => onCommand(camera)}>PTZ command</Button></CardContent></Card>
}

function CommandDialog({ target, onClose }: { target: Device | Camera; onClose: () => void }) {
  const [command, setCommand] = useState(target && 'camera_id' in target ? 'camera.ptz' : 'sensor.refresh'); const [result, setResult] = useState<string | null>(null); const [error, setError] = useState<string | null>(null); const [loading, setLoading] = useState(false)
  const submit = async () => { if (!window.confirm(`Send ${command} to ${'camera_id' in target ? target.camera_id : target.device_id}?`)) return; setLoading(true); setError(null); try { const response = 'camera_id' in target ? await api.cameraCommand(target.camera_id, command) : await api.command(target.device_id, command); setResult(`${response.status} (${response.request_id})`) } catch (reason) { setError(messageFor(reason)) } finally { setLoading(false) } }
  return <Paper elevation={6} sx={{ position: 'fixed', inset: 0, m: 'auto', width: 'min(420px, calc(100% - 32px))', height: 'fit-content', p: 3, zIndex: 10 }}><Typography variant="h6" gutterBottom>Send command</Typography><TextField fullWidth label="Command" value={command} onChange={(e) => setCommand(e.target.value)} helperText="Parameters can be added when the command schema is finalized." />{error && <Alert severity="error" sx={{ mt: 2 }}>{error}</Alert>}{result && <Alert severity="success" sx={{ mt: 2 }}>{result}</Alert>}<Stack direction="row" justifyContent="flex-end" spacing={1} sx={{ mt: 3 }}><Button onClick={onClose}>Close</Button><Button variant="contained" onClick={submit} disabled={loading || !command}>{loading ? 'Sending…' : 'Send'}</Button></Stack></Paper>
}

function Dashboard({ user, onLogout }: { user: User; onLogout: () => void }) {
  const [health, setHealth] = useState<LoadState<Health>>({ value: { status: 'checking' }, loading: true, error: null }); const [status, setStatus] = useState(initialLoad<SystemStatus>({})); const [devices, setDevices] = useState(initialLoad<Device[]>([])); const [cameras, setCameras] = useState(initialLoad<Camera[]>([])); const [selected, setSelected] = useState<Device | Camera | null>(null); const [details, setDetails] = useState<Device | null>(null); const [live, setLive] = useState('connecting'); const [reload, setReload] = useState(0)
  const load = useCallback(async <T,>(getter: () => Promise<T>, setter: (state: LoadState<T>) => void, empty: T) => { setter({ value: empty, loading: true, error: null }); try { setter({ value: await getter(), loading: false, error: null }) } catch (reason) { setter({ value: empty, loading: false, error: messageFor(reason) }) } }, [])
  useEffect(() => { load(api.health, setHealth, { status: 'offline' }) }, [load, reload]); useEffect(() => { load(api.status, setStatus, {}); load(api.devices, setDevices, []); load(api.cameras, setCameras, []) }, [load, reload])
  useEffect(() => { const socket = connectLiveEvents((event: LiveEvent) => { setLive('connected'); const device = event.type === 'device.state' || event.type === 'device.availability' ? eventDevice(event) : null; if (device?.device_id) setDevices((current) => ({ ...current, value: current.value.map((item) => item.device_id === device.device_id ? { ...item, ...device } : item) })); const reading = event.type === 'sensor.reading' ? eventReading(event) : null; if (reading) setDevices((current) => ({ ...current, value: current.value.map((item) => item.device_id === reading.deviceId ? { ...item, latest_reading: reading.reading } : item) })); const camera = event.type === 'camera.state' ? eventCamera(event) : null; if (camera?.camera_id) setCameras((current) => ({ ...current, value: current.value.map((item) => item.camera_id === camera.camera_id ? { ...item, ...camera } : item) })) }, { devices: devices.value.map((item) => item.device_id), cameras: cameras.value.map((item) => item.camera_id) }); socket.addEventListener('error', () => setLive('unavailable')); socket.addEventListener('close', () => setLive('unavailable')); return () => socket.close() }, [])
  const availableSensors = useMemo(() => devices.value.filter((device) => device.available).length, [devices.value]); const refresh = () => setReload((value) => value + 1)
  return <><AppBar position="static" elevation={0}><Toolbar><Typography variant="h6" sx={{ flexGrow: 1, fontWeight: 700 }}>my_assistant</Typography><Typography variant="body2" sx={{ mr: 2, display: { xs: 'none', sm: 'block' } }}>{user.username}</Typography><Tooltip title="Refresh"><IconButton color="inherit" onClick={refresh}>↻</IconButton></Tooltip><Tooltip title="Sign out"><IconButton color="inherit" onClick={onLogout}>⇥</IconButton></Tooltip></Toolbar></AppBar><Container maxWidth="xl" sx={{ py: { xs: 3, md: 5 } }}><Stack direction={{ xs: 'column', sm: 'row' }} justifyContent="space-between" gap={2} sx={{ mb: 3 }}><Box><Typography variant="h4" component="h1" fontWeight={700}>Home monitoring</Typography><Typography color="text.secondary">Sensors, cameras, and device control at a glance.</Typography></Box><Stack direction="row" gap={1} alignItems="center"><StatusChip online={health.value.status === 'ok'} label={health.loading ? 'Checking' : health.value.status === 'ok' ? 'Backend online' : 'Backend offline'} /><StatusChip online={live === 'connected'} label={`Live: ${live}`} /></Stack></Stack>{health.error && <ErrorState message={health.error} retry={refresh} />}<Grid container spacing={2} sx={{ mb: 4 }}><Grid item xs={12} sm={4}><Card><CardContent><Typography color="text.secondary">System status</Typography><Typography variant="h5" sx={{ mt: 1 }}>{status.loading ? 'Checking…' : status.error ? 'Unavailable' : status.value.backend || health.value.status}</Typography><Typography variant="body2" color="text.secondary">{status.error || `Live updates: ${live}`}</Typography></CardContent></Card></Grid><Grid item xs={12} sm={4}><Card><CardContent><Typography color="text.secondary">Sensors</Typography><Typography variant="h5" sx={{ mt: 1 }}>{availableSensors} / {devices.value.length || '—'} available</Typography><Typography variant="body2" color="text.secondary">Current availability</Typography></CardContent></Card></Grid><Grid item xs={12} sm={4}><Card><CardContent><Typography color="text.secondary">Cameras</Typography><Typography variant="h5" sx={{ mt: 1 }}>{cameras.value.filter((camera) => camera.available).length} / {cameras.value.length || '—'} online</Typography><Typography variant="body2" color="text.secondary">Stream access is session-protected</Typography></CardContent></Card></Grid></Grid><Typography variant="h5" sx={{ mb: 2 }}>Sensors</Typography>{devices.loading ? <LinearProgress /> : devices.error ? <ErrorState message={devices.error} /> : devices.value.length === 0 ? <Alert severity="info">No sensors have been discovered yet.</Alert> : <Grid container spacing={2} sx={{ mb: 4 }}>{devices.value.map((device) => <Grid item xs={12} sm={6} md={4} key={device.device_id}><SensorCard device={device} onCommand={setSelected} onDetails={setDetails} /></Grid>)}</Grid>}<Divider sx={{ mb: 3 }} /><Typography variant="h5" sx={{ mb: 2 }}>Cameras</Typography>{cameras.loading ? <LinearProgress /> : cameras.error ? <ErrorState message={cameras.error} /> : cameras.value.length === 0 ? <Alert severity="info">No cameras have been discovered yet.</Alert> : <Grid container spacing={2}>{cameras.value.map((camera) => <Grid item xs={12} md={6} key={camera.camera_id}><CameraCard camera={camera} onCommand={setSelected} /></Grid>)}</Grid>}</Container>{(selected || details) && <><Box onClick={() => { setSelected(null); setDetails(null) }} sx={{ position: 'fixed', inset: 0, bgcolor: 'rgba(0,0,0,.35)', zIndex: 9 }} />{selected && <CommandDialog target={selected} onClose={() => setSelected(null)} />}{details && <SensorDetails device={details} onClose={() => setDetails(null)} />}</>}</>
}

function App() { const [user, setUser] = useState<User | null>(null); const [checking, setChecking] = useState(true); useEffect(() => { api.me().then(setUser).catch(() => setUser(null)).finally(() => setChecking(false)) }, []); if (checking) return <Box sx={{ minHeight: '100vh', display: 'grid', placeItems: 'center' }}><CircularProgress /></Box>; if (!user) return <Login onLogin={setUser} />; return <Dashboard user={user} onLogout={() => api.logout().finally(() => setUser(null))} /> }
export default App
