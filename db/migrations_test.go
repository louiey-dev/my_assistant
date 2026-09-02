package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, "file:migrations-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations",
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	if count != len(names) {
		t.Fatalf("migration count = %d, want %d", count, len(names))
	}

	for _, table := range []string{"devices", "sensor_readings", "cameras", "users"} {
		var name string
		err := database.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s: %v", table, err)
		}
	}
}

func TestMigrateRejectsNilDatabase(t *testing.T) {
	if err := Migrate(context.Background(), (*sql.DB)(nil)); err == nil {
		t.Fatal("Migrate(nil) returned nil error")
	}
}

func TestBackupCanRestoreDatabase(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, "file:backup-source-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO users(username, password_hash, role, created_at, updated_at) VALUES ('backup-user', 'hash', 'viewer', 'now', 'now')"); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "my_assistant.sqlite3")
	if err := Backup(ctx, database, destination); err != nil {
		t.Fatal(err)
	}

	restored, err := Open(ctx, destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var username string
	if err := restored.QueryRowContext(ctx, "SELECT username FROM users WHERE user_id = 1").Scan(&username); err != nil {
		t.Fatal(err)
	}
	if username != "backup-user" {
		t.Fatalf("restored username = %q, want backup-user", username)
	}
}
