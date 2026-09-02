package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Backup writes a consistent SQLite snapshot to destination. The destination
// must not already exist; this prevents an accidental overwrite of a usable
// backup. The caller is responsible for encrypting and copying the snapshot
// to separate storage.
func Backup(ctx context.Context, database *sql.DB, destination string) error {
	if database == nil {
		return errors.New("database is nil")
	}
	if strings.TrimSpace(destination) == "" {
		return errors.New("backup destination is empty")
	}
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("backup destination already exists: %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check backup destination: %w", err)
	}

	// SQLite accepts a string literal here, so escape the path rather than
	// interpolating it raw. VACUUM INTO produces a consistent snapshot even
	// when the source database is in WAL mode.
	filename := strings.ReplaceAll(destination, "'", "''")
	if _, err := database.ExecContext(ctx, "VACUUM INTO '"+filename+"'"); err != nil {
		return fmt.Errorf("create database backup: %w", err)
	}
	return nil
}
