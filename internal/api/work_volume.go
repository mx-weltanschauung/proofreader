package api

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"proofreader/internal/models"
)

// volumeUniqueIndex is the partial unique index that keeps one work per
// (edition, volume, part). Its violation is a data conflict, not a server
// failure — the operator has to see that the volume is already taken.
const volumeUniqueIndex = "idx_works_edition_volume"

// isVolumeTakenError reports whether err is a unique violation (SQLSTATE
// 23505) of the edition/volume index specifically. Any other database error
// stays a 500.
func isVolumeTakenError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == volumeUniqueIndex
}

// multipartVolumes lists volumes published as several physical books, with the
// highest part number. Полутома есть у 25 (I—II), 26 (I—III) и 46 (I—II:
// «Экономические рукописи 1857—1859 годов», 1968 и 1969).
var multipartVolumes = map[int]int{25: 2, 26: 3, 46: 2}

// romanParts maps the accepted volume_part values to their ordinal.
var romanParts = map[string]int{"I": 1, "II": 2, "III": 3}

// volumeUpdate carries the volume coordinates of a PUT /works/{id} body
// together with the knowledge of which keys the client actually sent.
//
// The distinction matters: an absent key must preserve the stored value,
// while an explicit null clears it. Without it a save from the "Edit Work"
// form — which posts only title/author/language/country/status — would strip
// the volume coordinates and silently break index reference resolution.
type volumeUpdate struct {
	EditionID    *int64
	VolumeNumber *int
	VolumePart   *string
	PageOffset   *int

	HasEditionID    bool
	HasVolumeNumber bool
	HasVolumePart   bool
	HasPageOffset   bool
}

// parseVolumeUpdate extracts the volume coordinates from a raw request body,
// recording for each key whether it was present at all.
func parseVolumeUpdate(body []byte) (volumeUpdate, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return volumeUpdate{}, err
	}

	var u volumeUpdate
	// json.Unmarshal of a literal null into a pointer leaves it nil, which is
	// exactly the "clear this field" request.
	if v, ok := raw["edition_id"]; ok {
		if err := json.Unmarshal(v, &u.EditionID); err != nil {
			return volumeUpdate{}, fmt.Errorf("edition_id: %w", err)
		}
		u.HasEditionID = true
	}
	if v, ok := raw["volume_number"]; ok {
		if err := json.Unmarshal(v, &u.VolumeNumber); err != nil {
			return volumeUpdate{}, fmt.Errorf("volume_number: %w", err)
		}
		u.HasVolumeNumber = true
	}
	if v, ok := raw["volume_part"]; ok {
		if err := json.Unmarshal(v, &u.VolumePart); err != nil {
			return volumeUpdate{}, fmt.Errorf("volume_part: %w", err)
		}
		u.HasVolumePart = true
	}
	if v, ok := raw["page_offset"]; ok {
		if err := json.Unmarshal(v, &u.PageOffset); err != nil {
			return volumeUpdate{}, fmt.Errorf("page_offset: %w", err)
		}
		u.HasPageOffset = true
	}
	return u, nil
}

// applyVolumeFields merges the volume coordinates of an update onto the work.
// Keys the request did not carry keep their stored value; keys sent as null
// are cleared. Everything is validated against the resulting combination
// before anything is written, so a rejected request leaves the work untouched.
func applyVolumeFields(w *models.Work, u volumeUpdate) error {
	edition := w.EditionID
	if u.HasEditionID {
		edition = u.EditionID
	}
	number := w.VolumeNumber
	if u.HasVolumeNumber {
		number = u.VolumeNumber
	}
	part := w.VolumePart
	if u.HasVolumePart {
		part = u.VolumePart
	}
	offset := w.PageOffset
	if u.HasPageOffset {
		offset = 0
		if u.PageOffset != nil {
			offset = *u.PageOffset
		}
	}

	// An empty string is how a form clears the part; treat it as absence.
	if part != nil && *part == "" {
		part = nil
	}

	if number != nil && *number < 1 {
		return fmt.Errorf("volume_number must be positive, got %d", *number)
	}
	if part != nil {
		ord, ok := romanParts[*part]
		if !ok {
			return fmt.Errorf("volume_part must be I, II or III, got %q", *part)
		}
		if number == nil {
			return fmt.Errorf("volume_part %q without volume_number", *part)
		}
		maxPart, ok := multipartVolumes[*number]
		if !ok {
			return fmt.Errorf("volume %d has no parts", *number)
		}
		if ord > maxPart {
			return fmt.Errorf("volume %d has no part %q", *number, *part)
		}
	}

	w.EditionID = edition
	w.VolumeNumber = number
	w.VolumePart = part
	w.PageOffset = offset
	return nil
}
