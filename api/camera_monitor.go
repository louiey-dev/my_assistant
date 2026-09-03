package api

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"
)

// MonitorCameras periodically probes configured camera stream ports and
// persists their availability without opening a long-lived MJPEG connection.
func MonitorCameras(ctx context.Context, database *sql.DB, logger *slog.Logger, onEvent func(any)) {
	failures := make(map[string]int)
	states := make(map[string]bool)
	check := func() {
		rows, err := database.QueryContext(ctx, "SELECT camera_id, stream_config_ref FROM cameras WHERE TRIM(stream_config_ref) <> ''")
		if err != nil {
			if logger != nil {
				logger.Error("camera availability query failed", "event", "camera_availability_query_failed", "error", err)
			}
			return
		}
		cameras := make([]struct{ id, streamURL string }, 0)
		for rows.Next() {
			var id, streamURL string
			if err := rows.Scan(&id, &streamURL); err != nil {
				continue
			}
			cameras = append(cameras, struct{ id, streamURL string }{id: id, streamURL: streamURL})
		}
		_ = rows.Close()
		for _, camera := range cameras {
			id, streamURL := camera.id, camera.streamURL
			available := probeStream(ctx, streamURL)
			if available {
				failures[id] = 0
			} else {
				failures[id]++
				// Allow one transient probe failure, then report the camera
				// offline promptly so the browser can discard its old stream.
				if failures[id] < 2 {
					continue
				}
			}
			if previous, known := states[id]; known && previous == available {
				continue
			}
			states[id] = available
			if _, err := database.ExecContext(ctx, "UPDATE cameras SET available = ?, updated_at = ? WHERE camera_id = ?", available, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
				continue
			}
			if onEvent != nil {
				onEvent(map[string]any{"type": "camera.state", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "data": map[string]any{"camera_id": id, "available": available, "state": map[bool]string{true: "online", false: "offline"}[available]}})
			}
		}
	}

	check()
	// Do not repeatedly open raw TCP connections to a small ESP HTTP server.
	// Frequent probes can exhaust its limited socket pool and evict the active
	// MJPEG client. Frame-aware browser recovery handles stream failures.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

func probeStream(ctx context.Context, streamURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(streamURL))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(parsed.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}
