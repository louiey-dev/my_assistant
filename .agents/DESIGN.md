# my_assistant

Monitor approximately 30 sensors and stream approximately 10 cameras.
Control sensor boards and camera tilt/zoom through device-specific adapters.

## Project overview

- Room/office monitoring system accessed through a web dashboard.
- Development PC: WSL2 with Ubuntu 22.04.
- Hardware board: Raspberry Pi 4 running Ubuntu 22.04 ARM64.
- Raspberry PI IP address : pi@192.168.1.10
- The Raspberry Pi runs both the frontend and backend.
- Users access the dashboard over Ethernet or Wi-Fi.
- BLE sensors connect to the Go BLE adapter using BLE GATT.
- MQTT sensors connect through the Mosquitto broker.
- Use `.agents/TODOs.md` as the project task list.

## First-release decisions

- Backend: Go standard library `net/http`; add a router library only if the
  API becomes difficult to maintain with the standard library.
- Frontend: React with TypeScript and Vite.
- UI components: Material UI (MUI).
- Database: SQLite with WAL mode.
- API: REST for configuration and commands; WebSocket for live readings,
  availability, and command status.
- Message broker: Mosquitto using MQTT 5 where supported by clients.
- Camera gateway: go2rtc is the preferred option; validate it with the
  concurrent-camera test before finalizing the choice.
- Initial access model: LAN-only. Internet access requires an explicit future
  security review.
- API details: see [API design](doc/api_design_20260901_1635.md).

## Basic architecture

```text
MQTT sensors / MCU / ESP32 / nRF / TI
        │
        │ MQTT
        ▼
   Mosquitto broker
        │
        ▼
   Go backend ───── SQLite
        │
        │ REST API + WebSocket
        ▼
   React web dashboard

BLE sensors
        │
        │ BLE GATT
        ▼
   Go BLE adapter ───── Go backend

IP camera / Pi Camera
        │
        │ RTSP
        ▼
   go2rtc or MediaMTX (go2rtc preferred)
        │
        │ WebRTC/HLS
        ▼
   React web dashboard

React web dashboard
        │
        │ REST API + WebSocket
        ▼
   Go camera-control adapter
        │
        │ ONVIF/HTTP/MQTT (protocol TBD)
        ▼
   IP camera / Pi Camera
```

## Database

### Initial decision

Use SQLite for the first release.

Reasons:

- Approximately 30 sensors
- Single Raspberry Pi deployment
- Simple installation and backup
- Low operational overhead
- Suitable for sensor history and device configuration

### Migration criteria

Reconsider PostgreSQL if any of these become necessary:

- Multiple backend instances write to the database.
- Multiple users or services write data concurrently at a higher scale.
- Remote database access is required.
- Sensor history becomes too large for practical SQLite maintenance.
- High availability or replication is required.

### Data retention

- Store raw sensor readings for 90 days.
- Store hourly aggregates indefinitely.
- Provide a cleanup job for expired raw readings.
- Back up the database regularly.

### Backup and recovery

The backend can create a consistent SQLite snapshot with `db.Backup`; it does
not copy the live database file or its WAL files directly. Backups run once
per day after the database is migrated, with at least 14 daily snapshots and
one monthly snapshot retained. A failed backup is an operational error and
must not replace the last known-good backup.

The backup directory is temporary local staging only. A scheduled job copies
each completed snapshot to storage outside the Raspberry Pi, using encrypted
transport and encryption at rest. The off-device destination must have
restricted access and must not be the same physical disk as the live database.

Restore procedure:

1. Stop the backend and prevent it from restarting during the restore.
2. Preserve the current database and WAL/SHM files as an incident copy.
3. Verify the selected backup's checksum and copy it to a new database path.
4. Open the new path with the normal database initialization and run pending
   migrations.
5. Verify the migration history, users, devices, and recent sensor readings.
6. Update the configured database path, start the backend, and check health and
   authentication before reconnecting dashboard clients.

Restoration is tested by opening a generated snapshot as a fresh SQLite
database and checking representative application data. Backups never include
MQTT passwords or TLS private keys; those secrets are restored separately from
the deployment secret store.

### Initial schema

All schema changes must be versioned as migrations in `db/migrations/`. Enable
foreign keys and WAL mode when opening the database. Apply pending migrations
before starting the backend service.

```sql
CREATE TABLE devices (
    device_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    device_type TEXT NOT NULL,
    transport TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE sensor_readings (
    reading_id INTEGER PRIMARY KEY,
    device_id TEXT NOT NULL REFERENCES devices(device_id),
    measurement TEXT NOT NULL,
    value REAL NOT NULL,
    unit TEXT,
    recorded_at TEXT NOT NULL,
    received_at TEXT NOT NULL
);

CREATE INDEX sensor_readings_device_time_idx
    ON sensor_readings(device_id, recorded_at);

CREATE TABLE cameras (
    camera_id TEXT PRIMARY KEY,
    device_id TEXT REFERENCES devices(device_id),
    name TEXT NOT NULL,
    stream_config_ref TEXT NOT NULL,
    control_protocol TEXT,
    ptz_supported INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE users (
    user_id INTEGER PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'viewer',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

Use UTC ISO 8601 timestamps. Never store plaintext passwords or camera
credentials in the database. Store secrets in the deployment configuration or
an operating-system secret store.

## MQTT conventions

Use the following topic structure:

```text
my_assistant/v1/{device_id}/telemetry
my_assistant/v1/{device_id}/state
my_assistant/v1/{device_id}/availability
my_assistant/v1/{device_id}/discovery
my_assistant/v1/{device_id}/command
my_assistant/v1/{device_id}/command/{request_id}/result
```

Use JSON payloads with an ISO 8601 UTC timestamp, a device identifier, and a
schema version. Commands must include a request identifier so the dashboard
can match an asynchronous result to the original command.

### Device discovery

Devices publish a retained discovery message after connecting and whenever
their capabilities change. The backend subscribes to
`my_assistant/v1/+/discovery` and registers or updates the device record.

Example discovery payload:

```json
{
  "schema": 1,
  "device_id": "office-sensor-01",
  "name": "Office sensor 01",
  "device_type": "sensor",
  "transport": "mqtt",
  "firmware": "1.2.0",
  "capabilities": [
    {
      "id": "temperature_c",
      "kind": "measurement",
      "unit": "°C",
      "data_type": "number"
    },
    {
      "id": "humidity_percent",
      "kind": "measurement",
      "unit": "%",
      "data_type": "number"
    }
  ]
}
```

The discovery message must not contain passwords, private keys, RTSP
credentials, or other secrets. Devices publish `online` to the availability
topic on connection and configure an MQTT last-will message of `offline`.

Example telemetry payload:

```json
{
  "schema": 1,
  "device_id": "office-sensor-01",
  "timestamp": "2026-09-01T07:16:00Z",
  "measurements": {
    "temperature_c": 23.4,
    "humidity_percent": 48.1
  }
}
```

Example command payload:

```json
{
  "schema": 1,
  "request_id": "01JXYZ1234567890",
  "command": "camera.ptz",
  "parameters": {
    "pan_degrees": 10,
    "tilt_degrees": 0,
    "zoom": 1
  }
}
```

The camera-control adapter will translate the command to ONVIF, HTTP, MQTT,
or a vendor-specific protocol after the camera capability is identified.

### Camera stream access

Camera stream descriptor handlers must be wrapped with the authentication
middleware before they are registered, for example:

```go
mux.Handle("/api/v1/cameras/", authService.RequireSession(cameraHandler))
```

The backend must proxy or authorize go2rtc stream access rather than exposing
the camera gateway's administrative or unauthenticated listener to the LAN.
Stream responses must contain only short-lived, authorized playback details;
they must never contain RTSP credentials.

### Commands and results

The Go backend publishes commands to the device `command` topic. The device
publishes the result to the request-specific `result` topic. The dashboard
does not publish commands directly to MQTT.

Commands use QoS 1 and must not be retained. Telemetry may use QoS 0 or QoS 1.
Discovery, state, and availability messages are retained; command results are
not retained after the request is complete.

Example result payload:

```json
{
  "schema": 1,
  "device_id": "office-camera-01",
  "request_id": "01JXYZ1234567890",
  "timestamp": "2026-09-01T07:16:02Z",
  "status": "succeeded",
  "result": {
    "pan_degrees": 10,
    "tilt_degrees": 0,
    "zoom": 1
  }
}
```

Allowed status values are `accepted`, `running`, `succeeded`, `failed`,
`rejected`, and `timeout`. A failed or rejected result must include an
`error_code` and a safe human-readable `message`.

The backend must validate the command against the device discovery
capabilities before publishing it. Devices must treat `request_id` as an
idempotency key and avoid executing the same request more than once. The
backend marks a command as timed out if no terminal result is received within
the configured command timeout, initially 10 seconds.

## Security baseline

- Keep the first release LAN-only.
- Require authentication for the dashboard before exposing any device data or
  controls.
- Use separate MQTT credentials with topic ACLs.
- Keep camera credentials and application secrets outside source control.
- Require TLS before enabling access beyond the trusted LAN.

### Roles and permissions

The first release uses three roles. Authorization is enforced by the backend
after session authentication; the frontend must not be treated as an access
control boundary.

| Role | Read dashboard data | View camera streams | Send device commands | Manage devices/users |
| --- | --- | --- | --- | --- |
| `viewer` | Yes | Yes | No | No |
| `operator` | Yes | Yes | Yes | No |
| `admin` | Yes | Yes | Yes | Yes |

Unknown or empty roles are denied access to protected capabilities. Role
changes take effect on the next authenticated request because the role is
loaded from the database with the session's user.

### Credentials and configuration

Passwords are stored only as bcrypt hashes in SQLite. Database paths, session
policy, TLS paths, MQTT usernames/passwords, broker CA files, and camera
credentials are deployment configuration, never Go constants or committed
files. `.env.example` documents the variable names; the real `.env` is ignored
by Git. Production deployments should prefer root-readable files or an
operating-system secret store with least-privilege permissions.

### Network exposure and TLS

The first release is LAN-only: bind the application to the Raspberry Pi's
trusted LAN address or loopback behind a local reverse proxy, and do not
forward its ports from the router. Health checks may be reachable by local
service supervision but must not expose device data.

Any future internet or untrusted-network exposure requires HTTPS with a
trusted certificate, HTTP-to-HTTPS redirect, secure cookies, and disabled
plain-HTTP listeners. TLS certificate and key paths are supplied through
deployment configuration. The camera gateway's administrative and plain
HTTP listeners must remain private.

### MQTT transport and topic ACLs

The backend connects to Mosquitto with a dedicated identity, using TLS and a
CA-validated server certificate when the broker is not confined to the same
host. Passwords or client certificates are provisioned outside source code.
Anonymous MQTT access is disabled.

The backend identity is allowed to publish only:

```text
my_assistant/v1/+/command
```

and subscribe only to:

```text
my_assistant/v1/+/telemetry
my_assistant/v1/+/state
my_assistant/v1/+/availability
my_assistant/v1/+/discovery
my_assistant/v1/+/command/+/result
```

Device identities are restricted to their own device topics. Devices cannot
publish commands, cannot subscribe to another device's private topics, and
cannot access broker administration topics. Retained messages never contain
credentials or command payloads.

### Device command validation

Before publishing a command, the backend validates the JSON envelope, command
name, request ID, target device, and parameter types against the device's
recorded discovery capabilities. For `camera.ptz`, pan and tilt must be
finite values in `[-180, 180]` degrees and zoom must be a finite positive value
within the camera's advertised range. Unknown fields, unsupported commands,
unknown devices, malformed request IDs, and out-of-range values are rejected
with `422 unsupported_command` or `400 invalid_request`; none are published
to MQTT. The authenticated user's role is checked before validation and
publishing, and the request ID is retained as an idempotency key.

### Dashboard authentication

Use server-side sessions for the first release because the frontend and Go
backend run on the same Raspberry Pi and can use a same-origin interface.

- `POST /api/v1/auth/login` validates the username and password.
- `POST /api/v1/auth/logout` invalidates the current session.
- `GET /api/v1/auth/me` returns the authenticated user's identity and role.
- Store only a strong password hash in the `users.password_hash` column.
- Send the session identifier in a `Secure`, `HttpOnly`, `SameSite=Strict`
  cookie.
- Protect all `/api/v1/*` endpoints except health checks and authentication
  endpoints.
- Return `401 Unauthorized` for missing or invalid sessions.
- Return `403 Forbidden` when the authenticated user lacks permission.
- Rate-limit failed login attempts and log authentication failures without
  logging passwords, cookies, or session identifiers.
- Require a CSRF protection mechanism for state-changing cookie-authenticated
  requests if the deployment is not strictly same-origin.

### Logging and monitoring

Use Go's structured JSON `slog` logger. Authentication failures, MQTT
connection failures, camera connection failures, and backup failures include a
stable event name and safe diagnostic fields, but never passwords, cookies,
session identifiers, RTSP URLs, or private keys. Register
`monitoring.HealthHandler` at `/healthz` outside authentication; it returns
only `{"status":"ok"}` or `{"status":"unhealthy"}`.

The monitoring layer samples database size, filesystem capacity, Linux memory,
one-minute load, and thermal-zone temperature. The application should export
these values to its monitoring system at a future status endpoint. Log files
are rotated daily, retained for 14 rotations, and compressed using the
provided `deploy/my_assistant.logrotate` policy. Production deployments may
use journald instead, with an equivalent retention limit.

### Test coverage and prerequisites

The current test suite covers authentication REST handlers, session-backed
access and logout, failed-login rate limiting, protected camera stream boundaries,
database migrations, backup/restore, structured logging, and `/healthz`.

The remaining integration and scale tests are intentionally pending until the
corresponding runtime components are added: MQTT client and broker adapter,
WebSocket event server, camera gateway adapter, sensor ingestion loop, device
command publisher, and the Raspberry Pi service unit. Those tests must use a
broker fixture or test broker, deterministic fake clocks, fake camera/sensor
adapters, and isolated temporary databases so they do not depend on physical
hardware.
