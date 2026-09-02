// Package monitoring provides backend health, logging, and host metrics
// primitives for the application server and device adapters.
package monitoring

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// NewLogger returns the application's structured JSON logger.
func NewLogger(output io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level}))
}

// LogMQTTConnectionFailure records an MQTT connection failure without
// including credentials or connection URLs in the event.
func LogMQTTConnectionFailure(logger *slog.Logger, err error) {
	logFailure(logger, "mqtt connection failed", "mqtt_connection_failed", err)
}

// LogCameraConnectionFailure records a camera connection failure without
// including stream URLs or camera credentials in the event.
func LogCameraConnectionFailure(logger *slog.Logger, cameraID string, err error) {
	if logger == nil {
		return
	}
	logger.Error("camera connection failed", "event", "camera_connection_failed", "camera_id", cameraID, "error_type", errorType(err))
}

func logFailure(logger *slog.Logger, message, event string, err error) {
	if logger == nil {
		return
	}
	logger.Error(message, "event", event, "error_type", errorType(err))
}

func errorType(err error) string {
	if err == nil {
		return "unknown error"
	}
	return fmt.Sprintf("%T", err)
}

// HealthHandler returns a health endpoint suitable for /healthz. It does not
// require dashboard authentication and exposes only an overall status.
func HealthHandler(database *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if database == nil || database.PingContext(ctx) != nil {
			writeHealth(w, http.StatusServiceUnavailable, "unhealthy")
			return
		}
		writeHealth(w, http.StatusOK, "ok")
	})
}

func writeHealth(w http.ResponseWriter, status int, state string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": state})
}

// SystemStats contains best-effort host metrics. Fields unavailable on the
// current operating system are left at zero.
type SystemStats struct {
	DatabaseBytes        int64   `json:"database_bytes"`
	DiskAvailableBytes   uint64  `json:"disk_available_bytes"`
	DiskTotalBytes       uint64  `json:"disk_total_bytes"`
	MemoryTotalBytes     uint64  `json:"memory_total_bytes"`
	MemoryAvailableBytes uint64 `json:"memory_available_bytes"`
	Load1                float64 `json:"load_1m"`
	TemperatureCelsius   float64 `json:"temperature_celsius"`
}

// CollectSystemStats gathers database size and Linux host metrics. The path
// argument is also used to determine the filesystem being monitored.
func CollectSystemStats(databasePath string) (SystemStats, error) {
	var stats SystemStats
	if databasePath == "" {
		return stats, errors.New("database path is empty")
	}
	if info, err := os.Stat(databasePath); err == nil {
		stats.DatabaseBytes = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return stats, fmt.Errorf("stat database: %w", err)
	}

	var filesystem syscall.Statfs_t
	if err := syscall.Statfs(filepath.Dir(databasePath), &filesystem); err == nil {
		stats.DiskAvailableBytes = uint64(filesystem.Bavail) * uint64(filesystem.Bsize)
		stats.DiskTotalBytes = uint64(filesystem.Blocks) * uint64(filesystem.Bsize)
	}
	stats.MemoryTotalBytes, stats.MemoryAvailableBytes = linuxMemoryStats()
	stats.Load1 = linuxLoad1()
	stats.TemperatureCelsius = linuxTemperature()
	return stats, nil
}

func linuxMemoryStats() (uint64, uint64) {
	if runtime.GOOS != "linux" {
		return 0, 0
	}
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	values := make(map[string]uint64)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if value, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				values[strings.TrimSuffix(fields[0], ":")] = value * 1024
			}
		}
	}
	return values["MemTotal"], values["MemAvailable"]
}

func linuxLoad1() float64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	value, _ := strconv.ParseFloat(fields[0], 64)
	return value
}

func linuxTemperature() float64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	paths, _ := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err == nil {
			return value / 1000
		}
	}
	return 0
}
