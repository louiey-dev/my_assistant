# Basic architecture

```
Sensors / MCU / ESP32 / nRF / TI
        │
        │ MQTT
        ▼
   Mosquitto broker
        │
        ▼
   Go Backend ───── SQLite/PostgreSQL
        │
        │ WebSocket + REST API
        ▼
   React Web dashboard


IP Camera / Pi Camera
        │
        │ RTSP
        ▼
 go2rtc or MediaMTX (go2rtc prefered)
        │
        │ WebRTC/HLS
        ▼
   Web dashboard
```
---