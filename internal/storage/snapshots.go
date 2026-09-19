package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type Snapshot struct {
	Source    string          `json:"source"`
	Revision  string          `json:"revision,omitempty"`
	UpdatedAt string          `json:"updated_at"`
	Data      json.RawMessage `json:"data"`
}

// ReplaceSnapshot publishes a complete import in one statement, preserving the old
// snapshot if serialization or storage fails. Failed imports never call this method.
func (s *Store) ReplaceSnapshot(ctx context.Context, source, revision string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if len(raw) > 16*1024*1024 {
		return errors.New("snapshot exceeds storage budget")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO snapshots(source,revision,updated_at,data) VALUES(?,?,?,?) ON CONFLICT(source) DO UPDATE SET revision=excluded.revision,updated_at=excluded.updated_at,data=excluded.data`, source, revision, time.Now().UTC().Format(time.RFC3339), string(raw))
	return err
}
func (s *Store) Snapshot(ctx context.Context, source string) (Snapshot, error) {
	var out Snapshot
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT source,revision,updated_at,data FROM snapshots WHERE source=?`, source).Scan(&out.Source, &out.Revision, &out.UpdatedAt, &raw)
	out.Data = json.RawMessage(raw)
	return out, err
}
