package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store/db"
)

// MachineRecord is a machine as stored.
type MachineRecord struct {
	ID          string
	Name        string
	Runtimes    []runtime.Info
	ConnectedAt time.Time
	LastSeenAt  time.Time
	// DisconnectedAt is nil while the machine is connected.
	DisconnectedAt *time.Time
}

// RegisterMachine records that a machine connected and returns its ID.
//
// id is the ID the machine presented, or empty on its first connection. A
// known ID reconnects the existing row with a refreshed label and runtimes.
// An empty or unknown ID creates a new row, so a machine whose saved ID the
// hub no longer knows (for example after the database was reset) simply
// receives a fresh identity. A malformed ID is an error.
func (s *Store) RegisterMachine(ctx context.Context, id, name string, runtimes []runtime.Info) (string, error) {
	raw, err := marshalRuntimes(runtimes)
	if err != nil {
		return "", err
	}

	if id != "" {
		uid, err := parseUUID(id)
		if err != nil {
			return "", err
		}
		row, err := s.q.ReconnectMachine(ctx, db.ReconnectMachineParams{ID: uid, Name: name, Runtimes: raw})
		if err == nil {
			return uuidString(row.ID), nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("reconnect machine %s: %w", id, err)
		}
		// Unknown ID: fall through and register as new.
	}

	row, err := s.q.CreateMachine(ctx, db.CreateMachineParams{Name: name, Runtimes: raw})
	if err != nil {
		return "", fmt.Errorf("create machine %q: %w", name, err)
	}
	return uuidString(row.ID), nil
}

// TouchMachine records that the machine was heard from.
func (s *Store) TouchMachine(ctx context.Context, id string) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	if err := s.q.TouchMachine(ctx, uid); err != nil {
		return fmt.Errorf("touch machine %s: %w", id, err)
	}
	return nil
}

// UpdateMachineRuntimes replaces the machine's discovered runtimes.
func (s *Store) UpdateMachineRuntimes(ctx context.Context, id string, runtimes []runtime.Info) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	raw, err := marshalRuntimes(runtimes)
	if err != nil {
		return err
	}
	if err := s.q.UpdateMachineRuntimes(ctx, db.UpdateMachineRuntimesParams{ID: uid, Runtimes: raw}); err != nil {
		return fmt.Errorf("update runtimes of machine %s: %w", id, err)
	}
	return nil
}

// MarkMachineDisconnected records that the machine's connection ended.
func (s *Store) MarkMachineDisconnected(ctx context.Context, id string) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	if err := s.q.MarkMachineDisconnected(ctx, uid); err != nil {
		return fmt.Errorf("mark machine %s disconnected: %w", id, err)
	}
	return nil
}

// GetMachine returns one machine, or ErrNotFound.
func (s *Store) GetMachine(ctx context.Context, id string) (MachineRecord, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return MachineRecord{}, err
	}
	row, err := s.q.GetMachine(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return MachineRecord{}, fmt.Errorf("machine %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return MachineRecord{}, fmt.Errorf("get machine %s: %w", id, err)
	}
	return toMachineRecord(row)
}

// ListMachines returns every registered machine, connected or not, by name.
func (s *Store) ListMachines(ctx context.Context) ([]MachineRecord, error) {
	rows, err := s.q.ListMachines(ctx)
	if err != nil {
		return nil, fmt.Errorf("list machines: %w", err)
	}
	out := make([]MachineRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := toMachineRecord(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func toMachineRecord(row db.Machine) (MachineRecord, error) {
	var runtimes []runtime.Info
	if err := json.Unmarshal(row.Runtimes, &runtimes); err != nil {
		return MachineRecord{}, fmt.Errorf("decode runtimes of machine %s: %w", uuidString(row.ID), err)
	}
	rec := MachineRecord{
		ID:          uuidString(row.ID),
		Name:        row.Name,
		Runtimes:    runtimes,
		ConnectedAt: row.ConnectedAt.Time,
		LastSeenAt:  row.LastSeenAt.Time,
	}
	if row.DisconnectedAt.Valid {
		t := row.DisconnectedAt.Time
		rec.DisconnectedAt = &t
	}
	return rec, nil
}

// marshalRuntimes encodes runtimes for the jsonb column. A nil slice becomes
// an empty array rather than JSON null, so readers never see null.
func marshalRuntimes(runtimes []runtime.Info) ([]byte, error) {
	if runtimes == nil {
		runtimes = []runtime.Info{}
	}
	raw, err := json.Marshal(runtimes)
	if err != nil {
		return nil, fmt.Errorf("encode runtimes: %w", err)
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
