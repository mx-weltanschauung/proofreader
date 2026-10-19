package models

import "testing"

func TestShownVolumeNumber(t *testing.T) {
	n, part := 24, "III"
	w := Work{VolumeNumber: &n, VolumePart: &part}
	if got := w.ShownVolumeNumber(); got == nil || *got != 24 {
		t.Fatalf("печатная нумерация: номер %v, ожидалось 24", got)
	}
	if got := w.ShownVolumePart(); got == nil || *got != "III" {
		t.Fatalf("печатная нумерация: часть %v, ожидалось III", got)
	}
	w.VolumeOrderOnly = true
	if got := w.ShownVolumeNumber(); got != nil {
		t.Fatalf("условная нумерация: номер %d, ожидалось nil", *got)
	}
	if got := w.ShownVolumePart(); got != nil {
		t.Fatalf("условная нумерация: часть %q, ожидалось nil", *got)
	}
	if w.VolumeNumber == nil || *w.VolumeNumber != 24 {
		t.Fatal("сам номер тома (порядок полки) пропал")
	}
}

func TestValidVolumeNumbering(t *testing.T) {
	for v, want := range map[string]bool{"printed": true, "order": true, "": false, "Order": false} {
		if got := ValidVolumeNumbering(v); got != want {
			t.Errorf("ValidVolumeNumbering(%q) = %v, ожидалось %v", v, got, want)
		}
	}
}
