-- Migration 001: initial schema
-- Applied by the Go migration runner in a single transaction.

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
