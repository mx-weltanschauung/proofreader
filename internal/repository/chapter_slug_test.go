package repository

import (
	"testing"
	"time"
)

// stubRow подаёт значения в Scan по порядку — тот же приём, что позволяет
// проверять разбор строки без docker.
type stubRow []any

func (s stubRow) Scan(dest ...any) error {
	for i, d := range dest {
		switch p := d.(type) {
		case *int64:
			*p = s[i].(int64)
		case *string:
			*p = s[i].(string)
		case **string:
			*p = nil
		case **int64:
			*p = nil
		case *int:
			*p = s[i].(int)
		case *bool:
			*p = s[i].(bool)
		case *time.Time:
			*p = s[i].(time.Time)
		}
	}
	return nil
}

func TestScanChapterFillsSlug(t *testing.T) {
	now := time.Now()
	row := stubRow{int64(10125), int64(49), nil,
		"ЧТО ДЕЛАТЬ? Наболевшие вопросы нашего движения",
		"chapter", 1, 5, 200, false, nil, now, now}

	ch, err := scanChapter(row)
	if err != nil {
		t.Fatalf("разбор строки: %v", err)
	}
	if ch.Slug != "chto-delat-nabolevshie-voprosy-nashego-dvizheniya" {
		t.Errorf("Slug = %q", ch.Slug)
	}
}

func TestScanChapterLeavesEnumeratorWithoutSlug(t *testing.T) {
	now := time.Now()
	row := stubRow{int64(10240), int64(49), nil, "II", "chapter", 2, 6, 7, false, nil, now, now}

	ch, err := scanChapter(row)
	if err != nil {
		t.Fatalf("разбор строки: %v", err)
	}
	if ch.Slug != "" {
		t.Errorf("у главы-нумератора слаг %q, ожидался пустой", ch.Slug)
	}
}
