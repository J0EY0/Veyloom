package store

import "github.com/jackc/pgx/v5/pgtype"

// WikiScope says which wiki a turn reads and writes: the project's own, the
// skill library every project shares (docs/design.md 5.3, 5.10), or the
// personal memory, the one page of its own bundle (5.16).
type WikiScope string

const (
	WikiProject  WikiScope = "project"
	WikiLibrary  WikiScope = "library"
	WikiPersonal WikiScope = "personal"
)

// optionalUUID parses an id that may be empty, which is NULL.
func optionalUUID(s string) (pgtype.UUID, error) {
	if s == "" {
		return pgtype.UUID{}, nil
	}
	return parseUUID(s)
}
