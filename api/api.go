// Package api provides the dashboard's database-backed HTTP API.
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

type Handler struct{ DB *sql.DB }

func New(database *sql.DB) *Handler { return &Handler{DB: database} }

func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/status", h.status)
	mux.HandleFunc("GET /api/v1/devices", h.devices)
	mux.HandleFunc("GET /api/v1/devices/{device_id}/readings", h.readings)
	mux.HandleFunc("GET /api/v1/cameras", h.cameras)
	mux.HandleFunc("GET /api/v1/cameras/{camera_id}/stream", h.cameraStream)
	mux.HandleFunc("GET /api/v1/cameras/{camera_id}/stream/live", h.cameraLiveStream)
	mux.HandleFunc("POST /api/v1/devices/{device_id}/commands", h.deviceCommand)
	mux.HandleFunc("POST /api/v1/cameras/{camera_id}/commands", h.cameraCommand)
	return mux
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	database := "ok"
	if h.DB == nil || h.DB.PingContext(ctx) != nil {
		database = "unavailable"
	}
	writeJSON(w, http.StatusOK, map[string]string{"backend": "ok", "database": database, "broker": "unavailable", "streaming": "unavailable"})
}

func (h *Handler) devices(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `SELECT device_id, name, device_type, metadata_json FROM devices ORDER BY name, device_id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to list devices.")
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, deviceType, metadata string
		if err := rows.Scan(&id, &name, &deviceType, &metadata); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read devices.")
			return
		}
		item := map[string]any{"device_id": id, "name": name, "type": deviceType, "available": false, "state": "unknown"}
		var values map[string]any
		if json.Unmarshal([]byte(metadata), &values) == nil {
			if value, ok := values["available"].(bool); ok {
				item["available"] = value
			}
			if value, ok := values["state"].(string); ok {
				item["state"] = value
			}
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to list devices.")
		return
	}
	if err := rows.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to finish reading devices.")
		return
	}
	// SQLite is configured with one connection. Close the device rows before
	// querying the latest reading for each device, otherwise the nested query
	// waits forever for the connection held by rows.
	for _, item := range result {
		if id, ok := item["device_id"].(string); ok {
			item["latest_reading"] = h.latestReading(r.Context(), id)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) latestReading(ctx context.Context, deviceID string) map[string]any {
	var measurement, unit, timestamp string
	var value float64
	err := h.DB.QueryRowContext(ctx, `SELECT measurement, value, COALESCE(unit, ''), recorded_at FROM sensor_readings WHERE device_id = ? ORDER BY recorded_at DESC LIMIT 1`, deviceID).Scan(&measurement, &value, &unit, &timestamp)
	if err != nil {
		return nil
	}
	return map[string]any{"metric": measurement, "value": value, "unit": unit, "timestamp": timestamp}
}

func (h *Handler) readings(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("device_id")
	rows, err := h.DB.QueryContext(r.Context(), `SELECT measurement, value, COALESCE(unit, ''), recorded_at FROM sensor_readings WHERE device_id = ? ORDER BY recorded_at DESC LIMIT 200`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read sensor history.")
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var metric, unit, timestamp string
		var value float64
		if err := rows.Scan(&metric, &value, &unit, &timestamp); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read sensor history.")
			return
		}
		result = append(result, map[string]any{"metric": metric, "value": value, "unit": unit, "timestamp": timestamp})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read sensor history.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) cameras(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `SELECT camera_id, name, control_protocol, ptz_supported, available FROM cameras ORDER BY name, camera_id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to list cameras.")
		return
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id, name string
		var protocol sql.NullString
		var ptz, available bool
		if err := rows.Scan(&id, &name, &protocol, &ptz, &available); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read cameras.")
			return
		}
		result = append(result, map[string]any{"camera_id": id, "name": name, "available": available, "state": map[bool]string{true: "online", false: "offline"}[available], "capabilities": []string{protocol.String}})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to list cameras.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) cameraStream(w http.ResponseWriter, r *http.Request) {
	var name, streamURL string
	err := h.DB.QueryRowContext(r.Context(), "SELECT name, stream_config_ref FROM cameras WHERE camera_id = ?", r.PathValue("camera_id")).Scan(&name, &streamURL)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "The requested camera does not exist.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to read camera stream configuration.")
		return
	}
	if strings.TrimSpace(streamURL) == "" {
		writeError(w, http.StatusServiceUnavailable, "dependency_unavailable", "The camera stream is not configured.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"camera_id": r.PathValue("camera_id"), "name": name, "url": "/api/v1/cameras/" + r.PathValue("camera_id") + "/stream/live", "type": "mjpeg"})
}

func (h *Handler) cameraLiveStream(w http.ResponseWriter, r *http.Request) {
	var streamURL string
	err := h.DB.QueryRowContext(r.Context(), "SELECT stream_config_ref FROM cameras WHERE camera_id = ?", r.PathValue("camera_id")).Scan(&streamURL)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "The requested camera does not exist.")
		return
	}
	if err != nil || strings.TrimSpace(streamURL) == "" {
		writeError(w, http.StatusServiceUnavailable, "dependency_unavailable", "The camera stream is not configured.")
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, streamURL, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "The camera stream URL is invalid.")
		return
	}
	// Do not use the default client timeout here: an MJPEG stream is expected
	// to stay open indefinitely. The request context still closes the upstream
	// connection as soon as the browser disconnects.
	client := &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}}
	response, err := client.Do(request)
	if err != nil {
		writeError(w, http.StatusBadGateway, "camera_unavailable", "The camera stream could not be reached.")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		writeError(w, http.StatusBadGateway, "camera_unavailable", "The camera stream returned an error.")
		return
	}
	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "multipart/x-mixed-replace"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // useful when deployed behind nginx
	w.WriteHeader(response.StatusCode)

	// io.Copy can leave small MJPEG frames buffered by the HTTP server or a
	// reverse proxy. Flush after every upstream read so the browser receives
	// each frame promptly. A write error is normal when the browser reconnects.
	flusher, canFlush := w.(http.Flusher)
	buffer := make([]byte, 32*1024)
	for {
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			if _, writeErr := w.Write(buffer[:read]); writeErr != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			return
		}
	}
}

// RegisterCamera stores a non-secret stream descriptor for a camera. Stream
// credentials must never be embedded in the URL or returned to the browser.
func RegisterCamera(ctx context.Context, database *sql.DB, id, name, streamURL string) error {
	id, name, streamURL = strings.TrimSpace(id), strings.TrimSpace(name), strings.TrimSpace(streamURL)
	if id == "" || name == "" || streamURL == "" {
		return errors.New("camera id, name, and stream URL are required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := database.ExecContext(ctx, `INSERT INTO cameras(camera_id, name, stream_config_ref, control_protocol, ptz_supported, created_at, updated_at) VALUES (?, ?, ?, 'mjpeg', 0, ?, ?) ON CONFLICT(camera_id) DO UPDATE SET name=excluded.name, stream_config_ref=excluded.stream_config_ref, control_protocol=excluded.control_protocol, updated_at=excluded.updated_at`, id, name, streamURL, now, now)
	return err
}

func (h *Handler) deviceCommand(w http.ResponseWriter, r *http.Request) {
	h.command(w, r, "device_id", "devices", r.PathValue("device_id"))
}
func (h *Handler) cameraCommand(w http.ResponseWriter, r *http.Request) {
	h.command(w, r, "camera_id", "cameras", r.PathValue("camera_id"))
}

func (h *Handler) command(w http.ResponseWriter, r *http.Request, idColumn, table, id string) {
	var input struct {
		Command    string         `json:"command"`
		Parameters map[string]any `json:"parameters"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&input) != nil || strings.TrimSpace(input.Command) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "A command is required.")
		return
	}
	var exists int
	if err := h.DB.QueryRowContext(r.Context(), "SELECT 1 FROM "+table+" WHERE "+idColumn+" = ?", id).Scan(&exists); err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found", "The requested device does not exist.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to validate the command target.")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "dependency_unavailable", "The device command adapter is not available.")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
