package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/louiey-dev/my_assistant/api"
	"github.com/louiey-dev/my_assistant/auth"
	"github.com/louiey-dev/my_assistant/db"
	"github.com/louiey-dev/my_assistant/monitoring"
	"github.com/louiey-dev/my_assistant/mqtt"
	"github.com/louiey-dev/my_assistant/realtime"
	"github.com/louiey-dev/my_assistant/web"
)

func main() {
	logger := monitoring.NewLogger(os.Stderr, slog.LevelInfo)
	databasePath := env("MY_ASSISTANT_DATABASE", "./my_assistant.sqlite3")
	listenAddr := env("MY_ASSISTANT_LISTEN_ADDR", "127.0.0.1:8080")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	database, err := db.Open(ctx, databasePath)
	if err != nil {
		logger.Error("database initialization failed", "event", "database_init_failed", "error_type", errorType(err))
		os.Exit(1)
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		logger.Error("database migration failed", "event", "database_migration_failed", "error_type", errorType(err))
		os.Exit(1)
	}
	if len(os.Args) == 3 && os.Args[1] == "user" && os.Args[2] == "create" {
		if err := createUser(ctx, database); err != nil {
			fmt.Fprintln(os.Stderr, "user creation failed:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "user created")
		return
	}

	service := auth.New(database)
	dashboardAPI := api.New(database, logger)
	if cameraURL := strings.TrimSpace(os.Getenv("MY_ASSISTANT_CAMERA_STREAM_URL")); cameraURL != "" {
		cameraID := env("MY_ASSISTANT_CAMERA_ID", "esp32_camera01")
		cameraName := env("MY_ASSISTANT_CAMERA_NAME", "ESP32-S3-EYE")
		if err := api.RegisterCamera(ctx, database, cameraID, cameraName, cameraURL); err != nil {
			logger.Error("camera registration failed", "event", "camera_registration_failed", "error_type", errorType(err))
		}
	}
	eventHub := realtime.NewHub(logger)
	dashboardAPI.OnEvent = eventHub.Broadcast
	go api.MonitorCameras(ctx, database, logger, eventHub.Broadcast, dashboardAPI.IsStreamActive)
	if mqttURL := strings.TrimSpace(os.Getenv("MY_ASSISTANT_MQTT_URL")); mqttURL != "" {
		mqttClient := mqtt.NewClient(mqtt.Config{URL: mqttURL, Username: os.Getenv("MY_ASSISTANT_MQTT_USERNAME"), Password: os.Getenv("MY_ASSISTANT_MQTT_PASSWORD"), CAFile: os.Getenv("MY_ASSISTANT_MQTT_CA_FILE"), ClientID: "my_assistant-backend"}, mqtt.New(database), logger)
		mqttClient.OnEvent = eventHub.Broadcast
		go func() {
			if err := mqttClient.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("MQTT service stopped", "event", "mqtt_service_stopped", "error_type", errorType(err))
			}
		}()
	}
	service.SecureCookies = os.Getenv("MY_ASSISTANT_TLS_CERT_FILE") != "" && os.Getenv("MY_ASSISTANT_TLS_KEY_FILE") != ""
	mux := http.NewServeMux()
	mux.Handle("/healthz", monitoring.HealthHandler(database))
	mux.Handle("/api/v1/auth/", service.Handler())
	mux.Handle("/api/v1/ws", service.RequireSession(eventHub))
	mux.Handle("/api/v1/", service.RequireSession(dashboardAPI.Handler()))
	mux.Handle("/", web.Handler())

	server := &http.Server{Addr: listenAddr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	certFile, keyFile := os.Getenv("MY_ASSISTANT_TLS_CERT_FILE"), os.Getenv("MY_ASSISTANT_TLS_KEY_FILE")
	if (certFile == "") != (keyFile == "") {
		logger.Error("TLS requires both certificate and key files", "event", "tls_config_invalid")
		os.Exit(1)
	}
	logger.Info("backend started", "event", "backend_started", "listen_addr", listenAddr, "tls_enabled", certFile != "")
	var serveErr error
	if certFile != "" {
		serveErr = server.ListenAndServeTLS(certFile, keyFile)
	} else {
		serveErr = server.ListenAndServe()
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		logger.Error("backend stopped unexpectedly", "event", "backend_stopped", "error_type", errorType(serveErr))
		os.Exit(1)
	}
}

func createUser(ctx context.Context, database *sql.DB) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stdout, "Username: ")
	username, err := reader.ReadString('\n')
	if err != nil && len(username) == 0 {
		return err
	}
	fmt.Fprint(os.Stdout, "Password: ")
	password, err := readPassword(reader)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprint(os.Stdout, "Confirm password: ")
	confirmation, err := readPassword(reader)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout)
	if password != confirmation {
		return errors.New("passwords do not match")
	}
	return auth.CreateUser(ctx, database, strings.TrimSpace(username), password)
}

func readPassword(reader *bufio.Reader) (string, error) {
	fd := int(os.Stdin.Fd())
	term, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return "", fmt.Errorf("read terminal settings: %w", err)
	}
	original := *term
	term.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, term); err != nil {
		return "", err
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, &original)
	line, err := reader.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func errorType(err error) string {
	if err == nil {
		return "unknown error"
	}
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(fmt.Sprintf("%T", err), "*"), "errors."), "url.")
}
