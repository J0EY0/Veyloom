package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/store/db"
)

// WorkerRecord is a worker as stored.
type WorkerRecord struct {
	ID          string
	Name        string
	Engines     []engine.Info
	ConnectedAt time.Time
	LastSeenAt  time.Time
	// DisconnectedAt is nil while the worker is connected.
	DisconnectedAt *time.Time
}

// RegisterWorker records that a worker connected and returns its ID.
//
// id is the ID the worker presented, or empty on its first connection. A
// known ID reconnects the existing row with a refreshed label and engines.
// An empty or unknown ID creates a new row, so a worker whose saved ID the
// hub no longer knows (for example after the database was reset) simply
// receives a fresh identity. A malformed ID is an error.
func (s *Store) RegisterWorker(ctx context.Context, id, name string, engines []engine.Info) (string, error) {
	raw, err := marshalEngines(engines)
	if err != nil {
		return "", err
	}

	if id != "" {
		uid, err := parseUUID(id)
		if err != nil {
			return "", err
		}
		row, err := s.q.ReconnectWorker(ctx, db.ReconnectWorkerParams{ID: uid, Name: name, Engines: raw})
		if err == nil {
			return uuidString(row.ID), nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("reconnect worker %s: %w", id, err)
		}
		// Unknown ID: fall through and register as new.
	}

	row, err := s.q.CreateWorker(ctx, db.CreateWorkerParams{Name: name, Engines: raw})
	if err != nil {
		return "", fmt.Errorf("create worker %q: %w", name, err)
	}
	return uuidString(row.ID), nil
}

// TouchWorker records that the worker was heard from.
func (s *Store) TouchWorker(ctx context.Context, id string) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	if err := s.q.TouchWorker(ctx, uid); err != nil {
		return fmt.Errorf("touch worker %s: %w", id, err)
	}
	return nil
}

// UpdateWorkerEngines replaces the worker's discovered engines.
func (s *Store) UpdateWorkerEngines(ctx context.Context, id string, engines []engine.Info) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	raw, err := marshalEngines(engines)
	if err != nil {
		return err
	}
	if err := s.q.UpdateWorkerEngines(ctx, db.UpdateWorkerEnginesParams{ID: uid, Engines: raw}); err != nil {
		return fmt.Errorf("update engines of worker %s: %w", id, err)
	}
	return nil
}

// MarkWorkerDisconnected records that the worker's connection ended.
func (s *Store) MarkWorkerDisconnected(ctx context.Context, id string) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	if err := s.q.MarkWorkerDisconnected(ctx, uid); err != nil {
		return fmt.Errorf("mark worker %s disconnected: %w", id, err)
	}
	return nil
}

// GetWorker returns one worker, or ErrNotFound.
func (s *Store) GetWorker(ctx context.Context, id string) (WorkerRecord, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return WorkerRecord{}, err
	}
	row, err := s.q.GetWorker(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkerRecord{}, fmt.Errorf("worker %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return WorkerRecord{}, fmt.Errorf("get worker %s: %w", id, err)
	}
	return toWorkerRecord(row)
}

// ListWorkers returns every registered worker, connected or not, by name.
func (s *Store) ListWorkers(ctx context.Context) ([]WorkerRecord, error) {
	rows, err := s.q.ListWorkers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	out := make([]WorkerRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := toWorkerRecord(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func toWorkerRecord(row db.Worker) (WorkerRecord, error) {
	var engines []engine.Info
	if err := json.Unmarshal(row.Engines, &engines); err != nil {
		return WorkerRecord{}, fmt.Errorf("decode engines of worker %s: %w", uuidString(row.ID), err)
	}
	rec := WorkerRecord{
		ID:          uuidString(row.ID),
		Name:        row.Name,
		Engines:     engines,
		ConnectedAt: row.ConnectedAt.Time,
		LastSeenAt:  row.LastSeenAt.Time,
	}
	if row.DisconnectedAt.Valid {
		t := row.DisconnectedAt.Time
		rec.DisconnectedAt = &t
	}
	return rec, nil
}

// marshalEngines encodes engines for the jsonb column. A nil slice becomes
// an empty array rather than JSON null, so readers never see null.
func marshalEngines(engines []engine.Info) ([]byte, error) {
	if engines == nil {
		engines = []engine.Info{}
	}
	raw, err := json.Marshal(engines)
	if err != nil {
		return nil, fmt.Errorf("encode engines: %w", err)
	}
	return raw, nil
}

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, fmt.Errorf("%w: %q", ErrInvalidID, s)
	}
	return u, nil
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
