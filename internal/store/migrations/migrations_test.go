package migrations

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMigrations(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "migrations-test")
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 1. Test Initial Migrations
	m1 := []Migration{
		{Version: 1, SQL: "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)"},
		{Version: 2, SQL: "ALTER TABLE users ADD COLUMN email TEXT"},
	}

	if err := Run(ctx, db, m1); err != nil {
		t.Fatalf("initial run failed: %v", err)
	}

	// Verify table and column exist
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='users'").Scan(&name)
	if err != nil {
		t.Errorf("expected table 'users' to exist: %v", err)
	}

	// 2. Test Idempotency (Run again with same migrations)
	if err := Run(ctx, db, m1); err != nil {
		t.Fatalf("idempotent run failed: %v", err)
	}

	// 3. Test Incremental Migration
	m2 := append(m1, Migration{Version: 3, SQL: "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT)"})
	if err := Run(ctx, db, m2); err != nil {
		t.Fatalf("incremental run failed: %v", err)
	}

	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='posts'").Scan(&name)
	if err != nil {
		t.Errorf("expected table 'posts' to exist: %v", err)
	}

	// 4. Test Transactional Integrity (Rollback on failure)
	m3 := append(m2, Migration{Version: 4, SQL: "INVALID SQL STATEMENT"})
	err = Run(ctx, db, m3)
	if err == nil {
		t.Error("expected error for invalid migration, got nil")
	}

	// Verify version didn't increment to 4
	var currentVersion int
	db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&currentVersion)
	if currentVersion != 3 {
		t.Errorf("expected version 3 after failed migration, got %d", currentVersion)
	}
}
