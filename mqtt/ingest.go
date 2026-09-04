// Package mqtt contains the MQTT message ingestion boundary.
package mqtt

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Ingestor struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(database *sql.DB) *Ingestor { return &Ingestor{DB: database, Now: time.Now} }

// Handle processes one message from the my_assistant/v1/{device_id}/{kind}
// topic family. It is deliberately independent of a broker client so it can
// be tested and reused by reconnecting MQTT adapters.
func (i *Ingestor) Handle(ctx context.Context, topic string, payload []byte) error {
	parts := strings.Split(strings.Trim(topic, "/"), "/")
	if len(parts) != 4 || parts[0] != "my_assistant" || parts[1] != "v1" || strings.TrimSpace(parts[2]) == "" {
		return fmt.Errorf("unsupported MQTT topic: %s", topic)
	}
	switch parts[3] {
	case "discovery":
		return i.discovery(ctx, parts[2], payload)
	case "telemetry":
		return i.telemetry(ctx, parts[2], payload)
	case "availability":
		return i.availability(ctx, parts[2], payload)
	default:
		return fmt.Errorf("unsupported MQTT message kind: %s", parts[3])
	}
}

func (i *Ingestor) discovery(ctx context.Context, topicID string, payload []byte) error {
	var message struct {
		Schema     int    `json:"schema"`
		DeviceID   string `json:"device_id"`
		Name       string `json:"name"`
		DeviceType string `json:"device_type"`
		Transport  string `json:"transport"`
	}
	if err := json.Unmarshal(payload, &message); err != nil || message.DeviceID == "" || message.DeviceID != topicID || message.Name == "" || message.DeviceType == "" || message.Transport == "" {
		return errors.New("invalid discovery payload")
	}
	now := i.now().Format(time.RFC3339Nano)
	_, err := i.DB.ExecContext(ctx, `INSERT INTO devices(device_id, name, device_type, transport, metadata_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(device_id) DO UPDATE SET name=excluded.name, device_type=excluded.device_type, transport=excluded.transport, metadata_json=excluded.metadata_json, updated_at=excluded.updated_at`, message.DeviceID, message.Name, message.DeviceType, message.Transport, string(payload), now, now)
	return err
}

func (i *Ingestor) telemetry(ctx context.Context, topicID string, payload []byte) error {
	var message struct {
		Schema       int                `json:"schema"`
		DeviceID     string             `json:"device_id"`
		Timestamp    string             `json:"timestamp"`
		Measurements map[string]float64 `json:"measurements"`
	}
	if err := json.Unmarshal(payload, &message); err != nil || message.DeviceID == "" || message.DeviceID != topicID || len(message.Measurements) == 0 {
		return errors.New("invalid telemetry payload")
	}
	if _, err := i.device(ctx, message.DeviceID); err != nil {
		return err
	}
	recorded := message.Timestamp
	if _, err := time.Parse(time.RFC3339, recorded); err != nil {
		return errors.New("telemetry timestamp must be RFC3339")
	}
	received := i.now().Format(time.RFC3339Nano)
	for measurement, value := range message.Measurements {
		if _, err := i.DB.ExecContext(ctx, `INSERT INTO sensor_readings(device_id, measurement, value, unit, recorded_at, received_at) VALUES (?, ?, ?, NULL, ?, ?)`, message.DeviceID, measurement, value, recorded, received); err != nil {
			return err
		}
	}
	// Valid telemetry is direct evidence that the device is online. This also
	// repairs a stale retained "offline" message after a device reconnects.
	return i.setAvailability(ctx, message.DeviceID, true, received)
}

func (i *Ingestor) availability(ctx context.Context, topicID string, payload []byte) error {
	state := strings.TrimSpace(string(payload))
	if state != "online" && state != "offline" {
		return errors.New("availability must be online or offline")
	}
	return i.setAvailability(ctx, topicID, state == "online", i.now().Format(time.RFC3339Nano))
}

func (i *Ingestor) setAvailability(ctx context.Context, deviceID string, available bool, updatedAt string) error {
	var metadata string
	if err := i.DB.QueryRowContext(ctx, "SELECT metadata_json FROM devices WHERE device_id = ?", deviceID).Scan(&metadata); err != nil {
		return err
	}
	values := map[string]any{}
	_ = json.Unmarshal([]byte(metadata), &values)
	values["available"] = available
	if available {
		values["state"] = "online"
	} else {
		values["state"] = "offline"
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	_, err = i.DB.ExecContext(ctx, "UPDATE devices SET metadata_json = ?, updated_at = ? WHERE device_id = ?", string(encoded), updatedAt, deviceID)
	return err
}

func (i *Ingestor) device(ctx context.Context, id string) (string, error) {
	var found string
	err := i.DB.QueryRowContext(ctx, "SELECT device_id FROM devices WHERE device_id = ?", id).Scan(&found)
	return found, err
}
func (i *Ingestor) now() time.Time {
	if i.Now != nil {
		return i.Now().UTC()
	}
	return time.Now().UTC()
}
