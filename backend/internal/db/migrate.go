package db

import (
 "context"
 "database/sql"
 "embed"
 "fmt"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Apply(ctx context.Context, conn *sql.DB) error {
 body, err := migrationFiles.ReadFile("migrations/001_init.sql")
 if err != nil { return fmt.Errorf("read migration: %w", err) }
 if _, err := conn.ExecContext(ctx, string(body)); err != nil { return fmt.Errorf("apply migration: %w", err) }
 return nil
}
