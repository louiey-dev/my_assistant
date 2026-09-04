package mqtt

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/louiey-dev/my_assistant/db"
)

func TestIngestDiscoveryTelemetryAndAvailability(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, "file:mqtt-ingest-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	ingestor := New(database)
	ingestor.Now = func() time.Time { return time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC) }
	if err := ingestor.Handle(ctx, "my_assistant/v1/office-sensor-01/discovery", []byte(`{"schema":1,"device_id":"office-sensor-01","name":"Office sensor","device_type":"sensor","transport":"mqtt"}`)); err != nil {
		t.Fatal(err)
	}
	if err := ingestor.Handle(ctx, "my_assistant/v1/office-sensor-01/availability", []byte("online")); err != nil {
		t.Fatal(err)
	}
	if err := ingestor.Handle(ctx, "my_assistant/v1/office-sensor-01/telemetry", []byte(`{"schema":1,"device_id":"office-sensor-01","timestamp":"2026-09-02T00:00:00Z","measurements":{"temperature_c":23.4,"humidity_percent":48.1}}`)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM sensor_readings WHERE device_id = ?", "office-sensor-01").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("reading count = %d, want 2", count)
	}
	var metadata string
	if err := database.QueryRow("SELECT metadata_json FROM devices WHERE device_id = ?", "office-sensor-01").Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	if metadata == "" {
		t.Fatal("metadata is empty")
	}
	if !strings.Contains(metadata, `"available":true`) || !strings.Contains(metadata, `"state":"online"`) {
		t.Fatalf("metadata after telemetry = %s, want online availability", metadata)
	}
}

func TestTelemetryRepairsStaleOfflineAvailability(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, "file:mqtt-telemetry-availability-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	ingestor := New(database)
	if err := ingestor.Handle(ctx, "my_assistant/v1/sensor/discovery", []byte(`{"schema":1,"device_id":"sensor","name":"Sensor","device_type":"sensor","transport":"mqtt"}`)); err != nil {
		t.Fatal(err)
	}
	if err := ingestor.Handle(ctx, "my_assistant/v1/sensor/availability", []byte("offline")); err != nil {
		t.Fatal(err)
	}
	if err := ingestor.Handle(ctx, "my_assistant/v1/sensor/telemetry", []byte(`{"schema":1,"device_id":"sensor","timestamp":"2026-09-04T00:00:00Z","measurements":{"temperature_c":24}}`)); err != nil {
		t.Fatal(err)
	}
	var metadata string
	if err := database.QueryRow("SELECT metadata_json FROM devices WHERE device_id = ?", "sensor").Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(metadata, `"available":true`) || !strings.Contains(metadata, `"state":"online"`) {
		t.Fatalf("metadata after telemetry = %s, want online availability", metadata)
	}
}

func TestIngestRejectsUnknownDeviceTelemetry(t *testing.T) {
	database, err := db.Open(context.Background(), "file:mqtt-unknown-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	ingestor := New(database)
	if err := ingestor.Handle(context.Background(), "my_assistant/v1/missing/telemetry", []byte(`{"device_id":"missing","timestamp":"2026-09-02T00:00:00Z","measurements":{"x":1}}`)); err == nil {
		t.Fatal("unknown device telemetry accepted")
	}
}

func TestNormalizeBrokerURL(t *testing.T) {
	checks := []struct {
		input, want string
		tls         bool
	}{
		{"mqtt://127.0.0.1:1883", "tcp://127.0.0.1:1883", false},
		{"tls://127.0.0.1:8883", "ssl://127.0.0.1:8883", true},
		{"mqtts://broker.example:8883", "ssl://broker.example:8883", true},
	}
	for _, check := range checks {
		got, tls := normalizeBrokerURL(check.input)
		if got != check.want || tls != check.tls {
			t.Errorf("normalizeBrokerURL(%q) = (%q, %t), want (%q, %t)", check.input, got, tls, check.want, check.tls)
		}
	}
}
