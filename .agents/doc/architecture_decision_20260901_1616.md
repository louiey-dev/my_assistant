# Architecture decision record

Date: 2026-09-01

## Context

The first release targets approximately 30 sensors and 10 cameras on one
Raspberry Pi 4 running Ubuntu 22.04 ARM64. The application needs sensor
history, live updates, camera streaming, and device control.

## Decisions

| Area | First-release decision |
| --- | --- |
| Backend | Go standard library `net/http` |
| Frontend | React, TypeScript, and Vite |
| UI components | Material UI (MUI) |
| Database | SQLite with WAL mode |
| API | REST for configuration and commands; WebSocket for live state |
| MQTT broker | Mosquitto |
| Camera gateway | go2rtc provisionally; validate with load tests |
| Access model | LAN-only initially |

## Rationale

- A standard-library Go backend keeps the initial dependency and deployment
  footprint small on the Raspberry Pi.
- Vite provides a straightforward React and TypeScript development workflow.
- SQLite is appropriate for a single-board installation at the expected sensor
  scale. WAL mode supports concurrent reads while the backend writes data.
- Separating REST commands from WebSocket events keeps command handling and
  live updates easy to reason about.
- go2rtc is preferred for the first camera experiment, but the decision is not
  final until latency, CPU, memory, and ten-camera concurrency are tested.

## Not yet decided

- BLE GATT service and characteristic definitions
- Camera PTZ protocol
- Video recording and retention
- MQTT discovery and offline-device behavior
- Production authentication implementation and TLS deployment

See [DESIGN.md](../DESIGN.md) for the current architecture and
[TODOs.md](../TODOs.md) for implementation tasks.
