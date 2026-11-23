package repository

import (
	"context"
	"errors"
	"slices"
	"testing"

	"proofreader/internal/models"
)

func TestCollectionProgramKeepsItemIDsAndReadingOrder(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewCollectionRepository(pool)
	work := newVolume(t, NewWorkRepository(pool), "Том для встреч")
	collection := &models.Collection{Title: "Кружок", Slug: "kruzhok"}
	if err := repo.Create(ctx, collection); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for range 4 {
		item, err := repo.AddItem(ctx, collection.ID, models.CollectionItemKindWork, nil, &work.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, item.ID)
	}

	assertOrder := func(want ...int64) {
		t.Helper()
		rows, err := repo.ItemRows(ctx, collection.ID)
		if err != nil {
			t.Fatal(err)
		}
		var got []int64
		for i, row := range rows {
			got = append(got, row.Item.ID)
			if row.Item.OrderNumber != i+1 {
				t.Fatalf("позиция элемента %d = %d, нужна %d", row.Item.ID, row.Item.OrderNumber, i+1)
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("порядок = %v, нужен %v", got, want)
		}
	}
	assertOrder(ids...)

	revision := func() int64 {
		t.Helper()
		composition, err := repo.Composition(ctx, collection.ID)
		if err != nil {
			t.Fatal(err)
		}
		return composition.Revision
	}
	originalRevision := revision()

	first := models.CollectionMeetingInput{Title: "Первая встреча", Question: "Что обсудим?", ItemIDs: ids[:1]}
	second := models.CollectionMeetingInput{Title: "Вторая встреча", ItemIDs: []int64{ids[2], ids[1]}}
	if err := repo.SaveProgram(ctx, collection.ID, originalRevision, []models.CollectionMeetingInput{first, second}, ids[3:]); err != nil {
		t.Fatal(err)
	}
	assertOrder(ids[0], ids[2], ids[1], ids[3])

	if err := repo.SaveProgram(ctx, collection.ID, originalRevision, nil, ids); !errors.Is(err, ErrCollectionCompositionChanged) {
		t.Fatalf("устаревшая программа должна отклоняться: %v", err)
	}
	assertOrder(ids[0], ids[2], ids[1], ids[3])

	// Старый маршрут перестановки двигает текст в пределах его занятия.
	if err := repo.MoveItem(ctx, collection.ID, ids[0], 4); err != nil {
		t.Fatal(err)
	}
	assertOrder(ids[0], ids[2], ids[1], ids[3])

	composition, err := repo.Composition(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	meetings, rows := composition.Meetings, composition.Items
	if len(meetings) != 2 || meetings[0].Question != first.Question ||
		rows[1].Item.MeetingPosition == nil || *rows[1].Item.MeetingPosition != 2 ||
		rows[3].Item.MeetingPosition != nil {
		t.Fatalf("занятия = %+v, элементы = %+v", meetings, rows)
	}

	// Чужой ID или повтор вместо элемента состава не меняет программу.
	for _, invalid := range []int64{ids[1], ids[3] + 1000} {
		if err := repo.SaveProgram(ctx, collection.ID, revision(), []models.CollectionMeetingInput{second}, []int64{ids[0], invalid}); !errors.Is(err, ErrCollectionCompositionChanged) {
			t.Fatalf("запрос с неверным составом: %v", err)
		}
		assertOrder(ids[0], ids[2], ids[1], ids[3])
	}

	// Удаляем первое занятие, сохраняя его текст среди остальных.
	if err := repo.SaveProgram(ctx, collection.ID, revision(), []models.CollectionMeetingInput{second}, []int64{ids[3], ids[0]}); err != nil {
		t.Fatal(err)
	}
	assertOrder(ids[2], ids[1], ids[3], ids[0])
	if item, err := repo.ItemByID(ctx, collection.ID, ids[0]); err != nil || item.ID != ids[0] {
		t.Fatalf("элемент по прежнему адресу: %+v, %v", item, err)
	}
}
