package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationAndPersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Record(ctx, "github", "ready"); err != nil {
		t.Fatal(err)
	}
	if err = s.Record(ctx, "github", "failed"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	values, err := s.Observations(ctx)
	if err != nil || len(values) != 1 || values[0].Status != "failed" || values[0].CheckedAt == "" {
		t.Fatalf("unexpected values %v %v", values, err)
	}
}
func TestFutureSchemaRejected(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(context.Background(), dir); err == nil {
		s.Close()
		t.Fatal("accepted future schema")
	}
}
