# TODOs for my_assistant

Monitor approximately 30 sensors and stream approximately 10 cameras.
Control sensor boards and camera tilt/zoom through device-specific adapters.

## Architecture decisions

- [x] Select and document the Go backend framework
  - [x] Select the Go standard library `net/http` for the first release
  - [x] Record the decision in `DESIGN.md`

- [x] Select and document the React build system
  - [x] Select React with TypeScript and Vite
  - [x] Select Material UI (MUI)
  - [x] Record the decision in `DESIGN.md`

- [x] Select SQLite for the first release
  - [x] Record the decision and retention policy in `DESIGN.md`
- [x] Define the initial SQLite database schema in `DESIGN.md`
  - [x] Define device, sensor reading, camera, and user tables
  - [x] Add the initial versioned migration in `db/migrations/`
  - [x] Implement the Go migration runner
  - [x] Run migration tests with Go 1.25.11 in WSL

- [x] Define the initial API approach
  - [x] Use REST for configuration and commands
  - [x] Use WebSocket for live readings, availability, and command status
- [x] Define the API details in [API design](doc/api_design_20260901_1635.md)
  - [x] Define REST endpoints
  - [x] Define WebSocket events
  - [x] Define error response format
  - [x] Document the API

- [x] Define the initial MQTT conventions in `DESIGN.md`
  - [x] Define topic naming rules
  - [x] Define JSON payload structure
  - [x] Define device discovery messages
  - [x] Define command and response details

- [x] Select go2rtc as the provisional camera streaming server
  - [x] Record the provisional decision in `DESIGN.md`
  - [ ] Compare go2rtc and MediaMTX in a Raspberry Pi test
  - [ ] Test WebRTC latency
  - [ ] Test concurrent streaming for 10 cameras

## Security decisions

- [x] Use a LAN-only access model for the first release
- [x] Require authentication before enabling device controls
- [x] Use separate MQTT credentials and topic ACLs
- [x] Define dashboard authentication requirements in `DESIGN.md`
  - [x] Implement login, logout, and current-user endpoints
  - [x] Implement server-side session storage and expiration
  - [x] Add authentication middleware to protected API routes
  - [x] Add rate limiting for failed login attempts
- [x] Protect camera streams from unauthenticated access
- [x] Define user roles and permissions
- [x] Store credentials outside source code
- [x] Define whether the system is LAN-only or internet-accessible
- [x] Enable TLS for externally accessible services
- [x] Secure MQTT with username/password or certificates
- [x] Restrict MQTT topic permissions
- [x] Validate all device commands before publishing them

## Backup and recovery

- [x] Back up the SQLite database
- [x] Define backup frequency
- [x] Store backups outside the Raspberry Pi
- [x] Document database restore steps
- [x] Test restoration from a backup

## Logging and monitoring

- [x] Add structured backend logging
- [x] Log MQTT connection failures
- [x] Log camera connection failures
- [x] Add health-check endpoint
- [x] Monitor disk usage and database size
- [x] Monitor CPU, memory, and temperature
- [x] Configure log rotation

## Testing

- [x] Add backend unit tests
- [ ] Add MQTT integration tests
- [x] Add database tests
- [x] Add REST API tests
- [ ] Add WebSocket tests
- [ ] Test camera reconnect behavior
- [ ] Test Raspberry Pi restart behavior
- [ ] Test operation with 30 sensors
- [ ] Test concurrent streaming from 10 cameras
- [ ] Test invalid and unauthorized device commands

## Build and deployment

- [x] Set up the build system on WSL Ubuntu 22.04
  - [x] Configure cross-compilation
  - [ ] Create an ARM64 release package
    - [x] Build the backend for `linux/arm64`
    - [ ] Build frontend production assets
    - [x] Include a configuration template
    - [x] Install with one documented command
    - [x] Start automatically with systemd
    - [ ] Verify service recovery after Raspberry Pi reboot

## UI at frontend

- [ ] Implement the initial React + TypeScript + Vite frontend
  - [x] Create the frontend project structure
  - [ ] Install Material UI dependencies
  - [x] Add development and production build commands
  - [ ] Add a shared API client for REST requests and error responses
  - [ ] Add login, logout, session-check, and protected-route handling
  - [ ] Create the dashboard layout with navigation and system status
  - [ ] Display the health-check status and user/session state
  - [ ] Add a sensor list with availability, latest reading, and timestamp
  - [ ] Add a sensor detail view with historical readings and a basic graph
  - [ ] Add a camera list with online/offline status and a stream placeholder
  - [ ] Add device command controls with confirmation and result/error states
  - [ ] Add loading, empty, unauthorized, and backend-unavailable states
  - [ ] Connect live readings and command status through WebSocket events
  - [ ] Add responsive styling for desktop and tablet screens
  - [ ] Add frontend unit/component tests for authentication and dashboard states
  - [ ] Serve the built frontend from the Go backend
  - [ ] Include frontend production assets in the ARM64 release package
  - [ ] Deploy the UI to the Raspberry Pi and verify access over the LAN

## Open design questions

- [ ] Select and document the BLE protocol implementation
- [ ] Select the camera tilt/zoom protocol: ONVIF, HTTP, MQTT, or vendor-specific API
- [ ] Decide whether video recording is required
- [ ] Define the sensor backup restoration procedure
- [ ] Define MQTT reconnect and offline-device behavior
