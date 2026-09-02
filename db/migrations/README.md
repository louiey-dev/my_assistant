# Database migrations

Migration files are applied in filename order. Use a numeric prefix and never
edit a migration after it has been applied to a deployed database.

The Go migration runner in `db/migrations.go` must:

- Create a migration history table.
- Apply each pending migration in a transaction.
- Record the migration filename and applied timestamp.
- Stop on the first failed migration.
- Run migrations before starting the backend service.

SQLite must be opened with foreign-key enforcement enabled. Configure WAL mode
during database initialization, not inside an individual migration.

The initial schema is in
[001_initial_schema.sql](./001_initial_schema.sql). Authentication sessions
are added by [002_auth_sessions.sql](./002_auth_sessions.sql).
