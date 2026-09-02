import { useEffect, useState } from 'react'
import {
  AppBar,
  Box,
  Card,
  CardContent,
  Chip,
  Container,
  Grid,
  Toolbar,
  Typography,
} from '@mui/material'

type HealthState = 'checking' | 'online' | 'offline'

function App() {
  const [health, setHealth] = useState<HealthState>('checking')

  useEffect(() => {
    fetch('/healthz')
      .then((response) => {
        if (!response.ok) throw new Error('health check failed')
        setHealth('online')
      })
      .catch(() => setHealth('offline'))
  }, [])

  const healthLabel = health === 'checking' ? 'Checking' : health === 'online' ? 'Online' : 'Offline'
  const healthColor = health === 'online' ? 'success' : health === 'offline' ? 'error' : 'default'

  return (
    <Box sx={{ minHeight: '100vh' }}>
      <AppBar position="static" elevation={0}>
        <Toolbar>
          <Typography variant="h6" component="div" sx={{ flexGrow: 1, fontWeight: 700 }}>
            my_assistant
          </Typography>
          <Chip label={healthLabel} color={healthColor} variant="filled" />
        </Toolbar>
      </AppBar>

      <Container maxWidth="lg" sx={{ py: 4 }}>
        <Typography variant="h4" component="h1" gutterBottom>
          Home monitoring
        </Typography>
        <Typography color="text.secondary" sx={{ mb: 3 }}>
          Monitor sensors and cameras from your Raspberry Pi.
        </Typography>

        <Grid container spacing={3}>
          <Grid item xs={12} md={4}>
            <Card>
              <CardContent>
                <Typography color="text.secondary" gutterBottom>System status</Typography>
                <Typography variant="h5">{healthLabel}</Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                  Backend health endpoint
                </Typography>
              </CardContent>
            </Card>
          </Grid>
          <Grid item xs={12} md={4}>
            <Card>
              <CardContent>
                <Typography color="text.secondary" gutterBottom>Sensors</Typography>
                <Typography variant="h5">No sensors yet</Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                  Sensor discovery will appear here.
                </Typography>
              </CardContent>
            </Card>
          </Grid>
          <Grid item xs={12} md={4}>
            <Card>
              <CardContent>
                <Typography color="text.secondary" gutterBottom>Cameras</Typography>
                <Typography variant="h5">No cameras yet</Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                  Camera streams will appear here.
                </Typography>
              </CardContent>
            </Card>
          </Grid>
        </Grid>
      </Container>
    </Box>
  )
}

export default App
