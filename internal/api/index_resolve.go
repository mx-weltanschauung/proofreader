package api

import "proofreader/internal/models"

// volumeKey identifies a volume inside one edition. Part is "" when the volume
// was published as a single book.
type volumeKey struct {
	Number int
	Part   string
}

func keyOf(number int, part *string) volumeKey {
	if part == nil {
		return volumeKey{Number: number}
	}
	return volumeKey{Number: number, Part: *part}
}

// buildVolumeMap indexes an edition's volumes for lookup by (number, part).
func buildVolumeMap(locs []models.VolumeLocation) map[volumeKey]models.VolumeLocation {
	out := make(map[volumeKey]models.VolumeLocation, len(locs))
	for _, l := range locs {
		out[keyOf(l.VolumeNumber, l.VolumePart)] = l
	}
	return out
}

// resolveReference turns a printed page reference into a work page.
//
// The index addresses PRINTED folios; a work's pages are numbered by scan.
// The two differ by the volume's page_offset (печатная = page_number + offset),
// so the printed number goes back through that same relation. A volume that is
// not loaded, or a page outside the volume, resolves to nothing — the reference
// stays plain text and starts working by itself once the volume is loaded.
func resolveReference(ref *models.IndexReference, vols map[volumeKey]models.VolumeLocation) (int64, int, bool) {
	if ref.PageStart < 1 {
		return 0, 0, false
	}
	loc, ok := vols[keyOf(ref.VolumeNumber, ref.VolumePart)]
	if !ok {
		return 0, 0, false
	}
	page := ref.PageStart - loc.PageOffset
	if page < 1 || page > loc.MaxPage {
		return 0, 0, false
	}
	return loc.WorkID, page, true
}
