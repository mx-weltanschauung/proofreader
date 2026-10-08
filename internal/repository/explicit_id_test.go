package repository

import (
	"context"
	"errors"
	"testing"

	"proofreader/internal/models"
)

// Явный id нужен публикатору: id томов и изданий локально и на боевом
// совпадают, иначе ключ скана works/N/ разъезжается (спека
// 2026-10-03-local-scans-working-set, часть 3). Пол последовательностей в
// тестовой базе — fixtureIDFloor (1000).

func explicitWork(id int64, title string) *models.Work {
	return &models.Work{
		ID: id, Title: title, Author: "проба", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, OwnerID: 1,
	}
}

func TestWorkCreateWithExplicitIDKeepsSequenceAhead(t *testing.T) {
	ctx := context.Background()
	works := NewWorkRepository(testPool(t))

	w := explicitWork(5000, "явный")
	if err := works.Create(ctx, w); err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.ID != 5000 {
		t.Fatalf("id = %d, ждали 5000", w.ID)
	}
	next := explicitWork(0, "следующий")
	if err := works.Create(ctx, next); err != nil {
		t.Fatalf("create next: %v", err)
	}
	if next.ID != 5001 {
		t.Fatalf("следующий id = %d, ждали 5001 — последовательность не подтянулась", next.ID)
	}
}

// Явный id ниже последовательности не отматывает её назад: иначе следующая
// вставка без id упёрлась бы в уже выданное число.
func TestWorkCreateWithLowerExplicitIDDoesNotRewindSequence(t *testing.T) {
	ctx := context.Background()
	works := NewWorkRepository(testPool(t))

	if err := works.Create(ctx, explicitWork(10, "низкий")); err != nil {
		t.Fatalf("create: %v", err)
	}
	next := explicitWork(0, "следующий")
	if err := works.Create(ctx, next); err != nil {
		t.Fatalf("create next: %v", err)
	}
	if next.ID != fixtureIDFloor+1 {
		t.Fatalf("следующий id = %d, ждали %d", next.ID, fixtureIDFloor+1)
	}
}

func TestWorkCreateWithTakenIDIsErrIDTaken(t *testing.T) {
	ctx := context.Background()
	works := NewWorkRepository(testPool(t))

	if err := works.Create(ctx, explicitWork(7000, "первый")); err != nil {
		t.Fatalf("create: %v", err)
	}
	err := works.Create(ctx, explicitWork(7000, "второй"))
	if !errors.Is(err, ErrIDTaken) {
		t.Fatalf("второй с тем же id: %v; ждали ErrIDTaken", err)
	}
}

func TestEditionCreateWithExplicitID(t *testing.T) {
	ctx := context.Background()
	editions := NewEditionRepository(testPool(t))

	e := &models.Edition{ID: 3000, Title: "явное", Slug: "yavnoe"}
	if err := editions.Create(ctx, e); err != nil {
		t.Fatalf("create: %v", err)
	}
	if e.ID != 3000 {
		t.Fatalf("id = %d, ждали 3000", e.ID)
	}
	next := &models.Edition{Title: "следующее", Slug: "sleduyushchee"}
	if err := editions.Create(ctx, next); err != nil {
		t.Fatalf("create next: %v", err)
	}
	if next.ID != 3001 {
		t.Fatalf("следующий id = %d, ждали 3001", next.ID)
	}
	err := editions.Create(ctx, &models.Edition{ID: 3000, Title: "другое", Slug: "drugoe"})
	if !errors.Is(err, ErrIDTaken) {
		t.Fatalf("занятый id: %v; ждали ErrIDTaken", err)
	}
}
