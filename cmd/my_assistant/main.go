package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/louiey-dev/my_assistant/auth"
	"github.com/louiey-dev/my_assistant/db"
	"github.com/louiey-dev/my_assistant/monitoring"
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

	service := auth.New(database)
	service.SecureCookies = os.Getenv("MY_ASSISTANT_TLS_CERT_FILE") != "" && os.Getenv("MY_ASSISTANT_TLS_KEY_FILE") != ""
	mux := http.NewServeMux()
	mux.Handle("/healthz", monitoring.HealthHandler(database))
	mux.Handle("/api/v1/auth/", service.Handler())

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
