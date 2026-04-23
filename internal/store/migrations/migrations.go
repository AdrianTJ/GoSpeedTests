package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

type Migration struct {
	Version int
	SQL     string
}

// Run applies pending migrations to the SQLite database.
func Run(ctx context.Context, db *sql.DB, migrations []Migration) error {
	// 1. Ensure migrations table exists
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// 2. Get current version
	var currentVersion int
	err = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("failed to get current migration version: %w", err)
	}

	// 3. Run pending migrations
	for _, m := range migrations {
		if m.Version > currentVersion {
			log.Printf("Applying migration version %d", m.Version)
			
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}

			if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to apply migration %d: %w", m.Version, err)
			}

			if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, m.Version); err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to update migration version %d: %w", m.Version, err)
			}

			if err := tx.Commit(); err != nil {
				return err
			}
		}
	}

	return nil
}
