package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrScansHiddenStructure means a structural edit would remove a scan
// restriction without the administrator's separate reopening action.
var ErrScansHiddenStructure = errors.New("hidden scans prevent structural change")
var ErrRequiredInheritedScans = errors.New("inherited scan restriction is not active")

// The same transaction lock is acquired before scan toggles and structural
// mutations. It closes the gap between reading inherited access and writing
// new coordinates. These mutations are rare; one shared key keeps ordering
// simple across works, editions, journals and chapters.
func lockScanStructure(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(740157, 46)`); err != nil {
		return fmt.Errorf("lock scan visibility: %w", err)
	}
	return nil
}
