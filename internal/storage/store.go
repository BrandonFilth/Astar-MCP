// Package storage persists local provider observations without storing credentials.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }
type Observation struct {
	Provider  string `json:"provider"`
	Status    string `json:"status"`
	CheckedAt string `json:"checked_at"`
}

func Open(ctx context.Context, dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("MCP_DATA_DIR is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, errors.New("cannot create data directory")
	}
	file := filepath.Join(dir, "state.sqlite")
	db, err := sql.Open("sqlite", file)
	if err != nil {
		return nil, errors.New("cannot open storage")
	}
	db.SetMaxOpenConns(1)
	fail := func() (*Store, error) { db.Close(); return nil, errors.New("cannot migrate storage") }
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fail()
	}
	defer tx.Rollback()
	var version int
	failTx := func() (*Store, error) { _ = tx.Rollback(); return fail() }
	if tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version) != nil || version > 2 {
		return failTx()
	}
	if version == 0 {
		if _, err = tx.ExecContext(ctx, `CREATE TABLE observations (provider TEXT PRIMARY KEY, status TEXT NOT NULL, checked_at TEXT NOT NULL); PRAGMA user_version=1;`); err != nil {
			return failTx()
		}
	}
	if version < 2 {
		if _, err = tx.ExecContext(ctx, `CREATE TABLE snapshots (source TEXT PRIMARY KEY, revision TEXT NOT NULL, updated_at TEXT NOT NULL, data TEXT NOT NULL); PRAGMA user_version=2;`); err != nil {
			return failTx()
		}
	}
	if tx.Commit() != nil {
		return fail()
	}
	if os.Chmod(file, 0600) != nil {
		db.Close()
		return nil, errors.New("cannot secure storage")
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Record(ctx context.Context, provider, status string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO observations VALUES(?,?,?) ON CONFLICT(provider) DO UPDATE SET status=excluded.status,checked_at=excluded.checked_at`, provider, status, time.Now().UTC().Format(time.RFC3339))
	return err
}
func (s *Store) Observations(ctx context.Context) ([]Observation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider,status,checked_at FROM observations ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Observation{}
	for rows.Next() {
		var x Observation
		if err := rows.Scan(&x.Provider, &x.Status, &x.CheckedAt); err != nil {
			return nil, err
		}
		result = append(result, x)
	}
	return result, rows.Err()
}
