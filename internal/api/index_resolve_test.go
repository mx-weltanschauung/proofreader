package api

import (
	"testing"

	"proofreader/internal/models"
)

func TestResolveReference(t *testing.T) {
	part2 := "II"
	vols := buildVolumeMap([]models.VolumeLocation{
		{VolumeNumber: 4, WorkID: 4, PageOffset: 0, MaxPage: 640},
		{VolumeNumber: 5, WorkID: 5, PageOffset: 12, MaxPage: 700},
		{VolumeNumber: 23, WorkID: 23, PageOffset: -8, MaxPage: 900},
		{VolumeNumber: 25, VolumePart: &part2, WorkID: 52, PageOffset: 0, MaxPage: 500},
	})

	tests := []struct {
		name     string
		ref      models.IndexReference
		wantOK   bool
		wantWork int64
		wantPage int
	}{
		{
			name:     "том без оффсета",
			ref:      models.IndexReference{VolumeNumber: 4, PageStart: 85, PageEnd: 85},
			wantOK:   true,
			wantWork: 4,
			wantPage: 85,
		},
		{
			name:     "оффсет сдвигает печатную страницу",
			ref:      models.IndexReference{VolumeNumber: 23, PageStart: 46, PageEnd: 47},
			wantOK:   true,
			wantWork: 23,
			wantPage: 54, // 46 − (−8)
		},
		{
			name:     "полутом",
			ref:      models.IndexReference{VolumeNumber: 25, VolumePart: &part2, PageStart: 404, PageEnd: 404},
			wantOK:   true,
			wantWork: 52,
			wantPage: 404,
		},
		{
			name: "том не загружен",
			ref:  models.IndexReference{VolumeNumber: 31, PageStart: 268, PageEnd: 268},
		},
		{
			name: "полутом не совпадает с загруженным",
			ref:  models.IndexReference{VolumeNumber: 25, PageStart: 404, PageEnd: 404},
		},
		{
			name: "страница за пределами тома",
			ref:  models.IndexReference{VolumeNumber: 4, PageStart: 999, PageEnd: 999},
		},
		{
			name: "нечитаемая страница (page_start = 0)",
			ref:  models.IndexReference{VolumeNumber: 4, PageStart: 0, PageEnd: 0, IsUncertain: true},
		},
		{
			name: "оффсет уводит ниже первой страницы",
			ref:  models.IndexReference{VolumeNumber: 5, PageStart: 4, PageEnd: 4}, // 4 − 12 = −8
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workID, page, ok := resolveReference(&tt.ref, vols)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, ожидалось %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if workID != tt.wantWork || page != tt.wantPage {
				t.Fatalf("получено work=%d page=%d, ожидалось work=%d page=%d",
					workID, page, tt.wantWork, tt.wantPage)
			}
		})
	}
}
