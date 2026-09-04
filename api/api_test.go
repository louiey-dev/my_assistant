package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/louiey-dev/my_assistant/db"
)

func TestCameraAvailabilityOnlineWhileStreaming(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	database, err := db.Open(ctx, "file:camera-streaming-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}

	frameSent := make(chan struct{}, 1)
	fakeCamera := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_, _ = fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\n\r\nfake-jpeg\r\n")
				if flusher != nil {
					flusher.Flush()
				}
				select {
				case frameSent <- struct{}{}:
				default:
				}
			}
		}
	}))
	defer fakeCamera.Close()

	if err := RegisterCamera(ctx, database, "test_cam", "Test Camera", fakeCamera.URL); err != nil {
		t.Fatal(err)
	}
	// Force camera to be initially offline in DB.
	if _, err := database.ExecContext(ctx, "UPDATE cameras SET available = 0 WHERE camera_id = 'test_cam'"); err != nil {
		t.Fatal(err)
	}

	handler := New(database)
	var eventMu sync.Mutex
	var receivedEvents []map[string]any
	handler.OnEvent = func(event any) {
		eventMu.Lock()
		defer eventMu.Unlock()
		if m, ok := event.(map[string]any); ok {
			receivedEvents = append(receivedEvents, m)
		}
	}

	if handler.IsStreamActive("test_cam") {
		t.Fatal("expected IsStreamActive to be false before stream opens")
	}

	// GET /api/v1/cameras should report offline initially.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cameras", nil)
	rec := httptest.NewRecorder()
	handler.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cameras status = %d, want %d", rec.Code, http.StatusOK)
	}
	var cameras []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&cameras); err != nil {
		t.Fatal(err)
	}
	if len(cameras) != 1 || cameras[0]["available"] != false || cameras[0]["state"] != "offline" {
		t.Fatalf("unexpected initial camera list: %+v", cameras)
	}

	// Start reading the live stream through the proxy.
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	streamReq := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/test_cam/stream/live", nil).WithContext(streamCtx)
	streamReq.SetPathValue("camera_id", "test_cam")
	streamRec := httptest.NewRecorder()

	doneStreaming := make(chan struct{})
	go func() {
		defer close(doneStreaming)
		handler.Handler().ServeHTTP(streamRec, streamReq)
	}()

	select {
	case <-frameSent:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for camera frame")
	}

	// Allow a brief moment for the proxy to read and forward the frame.
	time.Sleep(50 * time.Millisecond)

	if !handler.IsStreamActive("test_cam") {
		t.Fatal("expected IsStreamActive to be true while streaming")
	}

	// Verify database was updated to available = 1.
	var dbAvailable bool
	if err := database.QueryRowContext(ctx, "SELECT available FROM cameras WHERE camera_id = 'test_cam'").Scan(&dbAvailable); err != nil {
		t.Fatal(err)
	}
	if !dbAvailable {
		t.Fatal("expected camera available to be true in database")
	}

	// GET /api/v1/cameras should now report online.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/cameras", nil)
	rec = httptest.NewRecorder()
	handler.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cameras status = %d, want %d", rec.Code, http.StatusOK)
	}
	if err := json.NewDecoder(rec.Body).Decode(&cameras); err != nil {
		t.Fatal(err)
	}
	if len(cameras) != 1 || cameras[0]["available"] != true || cameras[0]["state"] != "online" {
		t.Fatalf("unexpected active camera list: %+v", cameras)
	}

	// Verify WebSocket event was broadcast.
	eventMu.Lock()
	foundEvent := false
	for _, ev := range receivedEvents {
		if ev["type"] == "camera.state" {
			if data, ok := ev["data"].(map[string]any); ok && data["camera_id"] == "test_cam" && data["available"] == true {
				foundEvent = true
				break
			}
		}
	}
	eventMu.Unlock()
	if !foundEvent {
		t.Fatal("expected camera.state online event to be broadcast")
	}

	// Cancel stream and verify cleanup.
	cancelStream()
	<-doneStreaming
}

func TestMonitorCamerasSkipsActiveStreams(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	database, err := db.Open(ctx, "file:camera-monitor-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}

	// Register a camera with an unreachable stream URL. If probed via TCP, it would fail.
	if err := RegisterCamera(ctx, database, "active_cam", "Active Cam", "http://127.0.0.1:59999/stream"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "UPDATE cameras SET available = 1 WHERE camera_id = 'active_cam'"); err != nil {
		t.Fatal(err)
	}

	events := make(chan map[string]any, 10)
	onEvent := func(event any) {
		if m, ok := event.(map[string]any); ok {
			events <- m
		}
	}

	// When isStreamActive returns true, MonitorCameras must treat it as online and not probe.
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	defer stopMonitor()

	go MonitorCameras(monitorCtx, database, nil, onEvent, func(cameraID string) bool {
		return cameraID == "active_cam"
	})

	// Wait briefly for initial check to run.
	time.Sleep(100 * time.Millisecond)

	var available bool
	if err := database.QueryRowContext(ctx, "SELECT available FROM cameras WHERE camera_id = 'active_cam'").Scan(&available); err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("expected camera to remain online while stream is active")
	}

	// Verify no offline event was broadcast.
	select {
	case ev := <-events:
		if data, ok := ev["data"].(map[string]any); ok && data["available"] == false {
			t.Fatalf("unexpected offline event broadcast: %+v", ev)
		}
	default:
	}
}
