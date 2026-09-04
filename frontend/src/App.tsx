import { FormEvent, type Dispatch, type SetStateAction, useCallback, useEffect, useMemo, useRef, useState } from 'react'
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
  // Let the browser's native MJPEG decoder own the long-lived response.
  // Reading every frame through fetch creates large short-lived byte arrays
  // and has proved less reliable for multi-minute streams on Chromium.
  return <Box component="img" src={url} alt={alt} onError={onFailure} sx={{ width: '100%', height: '100%', objectFit: 'contain' }} />
}

function CameraCard({ camera, onCommand, onStreamingChange }: { camera: Camera; onCommand: (camera: Camera) => void; onStreamingChange: (cameraID: string, streaming: boolean) => void }) {
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
      onStreamingChange(camera.camera_id, true)
      retryDelay.current = 2000
    } catch (reason) {
      if (sequence !== loadSequence.current) return
      setStream(null)
      onStreamingChange(camera.camera_id, false)
      setStreamError(messageFor(reason))
      scheduleRetry()
    }
  }, [camera.camera_id, onStreamingChange])
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
    const recovered = camera.available === true && previousAvailability.current === false
    const firstLoad = previousAvailability.current === undefined
    previousAvailability.current = camera.available
    if (firstLoad || (recovered && stream === null)) {
      // The TCP availability probe is advisory: some small ESP camera servers
      // reject a second probe while serving video. Never tear down a working
      // MJPEG connection merely because that probe says Offline.
      if (retryTimer.current !== null) {
        window.clearTimeout(retryTimer.current)
        retryTimer.current = null
      }
      void restartStream()
    }
  }, [camera.available, restartStream, stream])
  // Only cancel a pending retry when the card is actually removed. Putting
  // this cleanup in the availability effect cancels recovery precisely when
  // the advisory probe changes to Offline.
  useEffect(() => {
    return () => {
      if (retryTimer.current !== null) window.clearTimeout(retryTimer.current)
      onStreamingChange(camera.camera_id, false)
    }
  }, [camera.camera_id, onStreamingChange])
  const retryStream = useCallback(() => {
    // The browser is the first component that can observe a broken MJPEG
    // connection. Show the offline state immediately instead of waiting for
    // the backend's slower availability probe.
    ++loadSequence.current
    setStream(null)
    onStreamingChange(camera.camera_id, false)
    setStreamError('Camera offline; waiting for it to reconnect…')
    scheduleRetry()
  }, [camera.camera_id, onStreamingChange, scheduleRetry])
  // Native MJPEG responses are intentionally endless, so browsers do not
  // consistently fire the image `load` event. A mounted stream with no error
  // is the reliable UI signal; onError removes it and starts reconnection.
  const online = camera.available === true || stream !== null
  return <Card sx={{ height: '100%' }}><Box sx={{ aspectRatio: '16 / 9', bgcolor: 'grey.900', display: 'grid', placeItems: 'center', color: 'grey.300', p: 2 }}>{stream ? stream.type === 'mjpeg' ? <MjpegStream key={stream.url} url={stream.url} onFailure={retryStream} alt={`${camera.name || camera.camera_id} stream`} /> : <Box key={stream.url} component="video" src={stream.url} onError={retryStream} onAbort={retryStream} controls sx={{ width: '100%', height: '100%' }} /> : <Stack alignItems="center" spacing={1}><CircularProgress size={24} color="inherit" /><Typography>{streamError ? 'Camera interrupted; reconnecting…' : 'Connecting to camera…'}</Typography><Button variant="outlined" color="inherit" size="small" onClick={restartStream}>Retry now</Button></Stack>}</Box><CardContent><Stack direction="row" justifyContent="space-between" gap={1}><Box><Typography variant="h6">{camera.name || camera.camera_id}</Typography><Typography variant="caption" color="text.secondary">{camera.camera_id}</Typography></Box><StatusChip online={online} /></Stack>{streamError && stream && <Alert severity="info" sx={{ mt: 1 }}>{streamError}</Alert>}<Button sx={{ mt: 1 }} size="small" variant="outlined" disabled={!online} onClick={() => onCommand(camera)}>PTZ command</Button></CardContent></Card>
}

function CommandDialog({ target, onClose }: { target: Device | Camera; onClose: () => void }) {
  const [command, setCommand] = useState(target && 'camera_id' in target ? 'camera.ptz' : 'sensor.refresh'); const [result, setResult] = useState<string | null>(null); const [error, setError] = useState<string | null>(null); const [loading, setLoading] = useState(false)
  const submit = async () => { if (!window.confirm(`Send ${command} to ${'camera_id' in target ? target.camera_id : target.device_id}?`)) return; setLoading(true); setError(null); try { const response = 'camera_id' in target ? await api.cameraCommand(target.camera_id, command) : await api.command(target.device_id, command); setResult(`${response.status} (${response.request_id})`) } catch (reason) { setError(messageFor(reason)) } finally { setLoading(false) } }
  return <Paper elevation={6} sx={{ position: 'fixed', inset: 0, m: 'auto', width: 'min(420px, calc(100% - 32px))', height: 'fit-content', p: 3, zIndex: 10 }}><Typography variant="h6" gutterBottom>Send command</Typography><TextField fullWidth label="Command" value={command} onChange={(e) => setCommand(e.target.value)} helperText="Parameters can be added when the command schema is finalized." />{error && <Alert severity="error" sx={{ mt: 2 }}>{error}</Alert>}{result && <Alert severity="success" sx={{ mt: 2 }}>{result}</Alert>}<Stack direction="row" justifyContent="flex-end" spacing={1} sx={{ mt: 3 }}><Button onClick={onClose}>Close</Button><Button variant="contained" onClick={submit} disabled={loading || !command}>{loading ? 'Sending…' : 'Send'}</Button></Stack></Paper>
}

function Dashboard({ user, onLogout }: { user: User; onLogout: () => void }) {
  const [health, setHealth] = useState<LoadState<Health>>({ value: { status: 'checking' }, loading: true, error: null }); const [status, setStatus] = useState(initialLoad<SystemStatus>({})); const [devices, setDevices] = useState(initialLoad<Device[]>([])); const [cameras, setCameras] = useState(initialLoad<Camera[]>([])); const [selected, setSelected] = useState<Device | Camera | null>(null); const [details, setDetails] = useState<Device | null>(null); const [live, setLive] = useState('connecting'); const [reload, setReload] = useState(0)
  const [streamingCameras, setStreamingCameras] = useState<Set<string>>(() => new Set())
  const load = useCallback(async <T,>(getter: () => Promise<T>, setter: Dispatch<SetStateAction<LoadState<T>>>, empty: T) => { setter((previous) => ({ value: previous.value ?? empty, loading: true, error: null })); try { setter({ value: await getter(), loading: false, error: null }) } catch (reason) { setter((previous) => ({ value: previous.value ?? empty, loading: false, error: messageFor(reason) })) } }, [])
  const refresh = useCallback(() => setReload((value) => value + 1), [])
  const setCameraStreaming = useCallback((cameraID: string, streaming: boolean) => {
    setStreamingCameras((current) => {
      if (current.has(cameraID) === streaming) return current
      const next = new Set(current)
      if (streaming) next.add(cameraID)
      else next.delete(cameraID)
      return next
    })
  }, [])
  useEffect(() => { load(api.health, setHealth, { status: 'offline' }) }, [load, reload]); useEffect(() => { load(api.status, setStatus, {}); load(api.devices, setDevices, []); load(api.cameras, setCameras, []) }, [load, reload])
  useEffect(() => { const disconnect = connectLiveEvents((event: LiveEvent) => { const device = event.type === 'device.state' || event.type === 'device.availability' ? eventDevice(event) : null; if (device?.device_id) setDevices((current) => ({ ...current, value: current.value.map((item) => item.device_id === device.device_id ? { ...item, ...device } : item) })); const reading = event.type === 'sensor.reading' ? eventReading(event) : null; if (reading) setDevices((current) => ({ ...current, value: current.value.map((item) => item.device_id === reading.deviceId ? { ...item, available: true, state: 'online', latest_reading: reading.reading } : item) })); const camera = event.type === 'camera.state' ? eventCamera(event) : null; if (camera?.camera_id) setCameras((current) => ({ ...current, value: current.value.map((item) => item.camera_id === camera.camera_id ? { ...item, ...camera } : item) })) }, { devices: [], cameras: [] }, (state, reconnected) => { setLive(state); if (state === 'connected' && reconnected) refresh() }); return disconnect }, [refresh])
  const availableSensors = useMemo(() => devices.value.filter((device) => device.available).length, [devices.value])
  const availableCameras = useMemo(() => cameras.value.filter((camera) => camera.available || streamingCameras.has(camera.camera_id)).length, [cameras.value, streamingCameras])
  return <><AppBar position="static" elevation={0}><Toolbar><Typography variant="h6" sx={{ flexGrow: 1, fontWeight: 700 }}>my_assistant</Typography><Typography variant="body2" sx={{ mr: 2, display: { xs: 'none', sm: 'block' } }}>{user.username}</Typography><Tooltip title="Refresh"><IconButton color="inherit" onClick={refresh}>↻</IconButton></Tooltip><Tooltip title="Sign out"><IconButton color="inherit" onClick={onLogout}>⇥</IconButton></Tooltip></Toolbar></AppBar><Container maxWidth="xl" sx={{ py: { xs: 3, md: 5 } }}><Stack direction={{ xs: 'column', sm: 'row' }} justifyContent="space-between" gap={2} sx={{ mb: 3 }}><Box><Typography variant="h4" component="h1" fontWeight={700}>Home monitoring</Typography><Typography color="text.secondary">Sensors, cameras, and device control at a glance.</Typography></Box><Stack direction="row" gap={1} alignItems="center"><StatusChip online={health.value.status === 'ok'} label={health.loading ? 'Checking' : health.value.status === 'ok' ? 'Backend online' : 'Backend offline'} /><StatusChip online={live === 'connected'} label={`Live: ${live}`} /></Stack></Stack>{health.error && <ErrorState message={health.error} retry={refresh} />}<Grid container spacing={2} sx={{ mb: 4 }}><Grid item xs={12} sm={4}><Card><CardContent><Typography color="text.secondary">System status</Typography><Typography variant="h5" sx={{ mt: 1 }}>{status.loading ? 'Checking…' : status.error ? 'Unavailable' : status.value.backend || health.value.status}</Typography><Typography variant="body2" color="text.secondary">{status.error || `Live updates: ${live}`}</Typography></CardContent></Card></Grid><Grid item xs={12} sm={4}><Card><CardContent><Typography color="text.secondary">Sensors</Typography><Typography variant="h5" sx={{ mt: 1 }}>{availableSensors} / {devices.value.length || '—'} available</Typography><Typography variant="body2" color="text.secondary">Current availability</Typography></CardContent></Card></Grid><Grid item xs={12} sm={4}><Card><CardContent><Typography color="text.secondary">Cameras</Typography><Typography variant="h5" sx={{ mt: 1 }}>{availableCameras} / {cameras.value.length || '—'} online</Typography><Typography variant="body2" color="text.secondary">Stream access is session-protected</Typography></CardContent></Card></Grid></Grid><Typography variant="h5" sx={{ mb: 2 }}>Sensors</Typography>{devices.loading ? <LinearProgress /> : devices.error ? <ErrorState message={devices.error} /> : devices.value.length === 0 ? <Alert severity="info">No sensors have been discovered yet.</Alert> : <Grid container spacing={2} sx={{ mb: 4 }}>{devices.value.map((device) => <Grid item xs={12} sm={6} md={4} key={device.device_id}><SensorCard device={device} onCommand={setSelected} onDetails={setDetails} /></Grid>)}</Grid>}<Divider sx={{ mb: 3 }} /><Typography variant="h5" sx={{ mb: 2 }}>Cameras</Typography>{cameras.loading ? <LinearProgress /> : cameras.error ? <ErrorState message={cameras.error} /> : cameras.value.length === 0 ? <Alert severity="info">No cameras have been discovered yet.</Alert> : <Grid container spacing={2}>{cameras.value.map((camera) => <Grid item xs={12} md={6} key={camera.camera_id}><CameraCard camera={camera} onCommand={setSelected} onStreamingChange={setCameraStreaming} /></Grid>)}</Grid>}</Container>{(selected || details) && <><Box onClick={() => { setSelected(null); setDetails(null) }} sx={{ position: 'fixed', inset: 0, bgcolor: 'rgba(0,0,0,.35)', zIndex: 9 }} />{selected && <CommandDialog target={selected} onClose={() => setSelected(null)} />}{details && <SensorDetails device={details} onClose={() => setDetails(null)} />}</>}</>
}

function App() { const [user, setUser] = useState<User | null>(null); const [checking, setChecking] = useState(true); useEffect(() => { api.me().then(setUser).catch(() => setUser(null)).finally(() => setChecking(false)) }, []); if (checking) return <Box sx={{ minHeight: '100vh', display: 'grid', placeItems: 'center' }}><CircularProgress /></Box>; if (!user) return <Login onLogin={setUser} />; return <Dashboard user={user} onLogout={() => api.logout().finally(() => setUser(null))} /> }
export default App
