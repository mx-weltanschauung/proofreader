package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"proofreader/internal/models"
)

func TestCollectionProgramReviewWorkflow(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewCollectionRepository(pool)
	ownerID := int64(1)
	collection := &models.Collection{OwnerID: &ownerID, Title: "Программа", Slug: "program", AuthorNickname: "author"}
	if err := repo.Create(ctx, collection); err != nil {
		t.Fatal(err)
	}
	meetings := []models.CollectionMeetingInput{{Title: "Занятие", Question: "К обсуждению"}}
	if err := repo.SaveProgram(ctx, collection.ID, 0, meetings, nil); err != nil {
		t.Fatal(err)
	}
	since := time.Now().Add(-24 * time.Hour)
	if _, _, err := repo.PublishWithinLimit(ctx, collection.ID, "ip", 3, since); !errors.Is(err, ErrCollectionProgramReviewRequired) {
		t.Fatalf("прямая публикация: %v", err)
	}
	if submitted, err := repo.SubmitProgram(ctx, collection.ID, 1, "ip", 3, since); err != nil || !submitted {
		t.Fatalf("подача: %v, %v", submitted, err)
	}
	queue, err := repo.ProgramsForReview(ctx)
	if err != nil || len(queue) != 1 || queue[0].ID != collection.ID || queue[0].Revision != 2 {
		t.Fatalf("очередь: %+v, %v", queue, err)
	}
	if err := repo.Update(ctx, collection); !errors.Is(err, ErrCollectionProgramPending) {
		t.Fatalf("правка заголовка после подачи: %v", err)
	}
	if err := repo.SaveProgram(ctx, collection.ID, 2, nil, nil); !errors.Is(err, ErrCollectionProgramPending) {
		t.Fatalf("удаление занятий после подачи: %v", err)
	}

	if err := repo.ResolveProgram(ctx, collection.ID, 2, "Уточните вопрос"); err != nil {
		t.Fatal(err)
	}
	rejected, err := repo.GetByAuthorSlug(ctx, "author", "program")
	if err != nil || rejected.ProgramReviewStatus != models.CollectionProgramRejected ||
		rejected.ProgramRejectReason != "Уточните вопрос" || rejected.PublishedAt != nil {
		t.Fatalf("отклонённая программа: %+v, %v", rejected, err)
	}
	if err := repo.ResolveProgram(ctx, collection.ID, 2, ""); !errors.Is(err, ErrCollectionProgramNotPending) {
		t.Fatalf("решение по уже отклонённой версии: %v", err)
	}

	meetings[0].Question = "Уточнённый вопрос"
	if err := repo.SaveProgram(ctx, collection.ID, rejected.Revision, meetings, nil); err != nil {
		t.Fatal(err)
	}
	if submitted, err := repo.SubmitProgram(ctx, collection.ID, rejected.Revision+1, "ip", 3, since); err != nil || !submitted {
		t.Fatalf("повторная подача: %v, %v", submitted, err)
	}
	if err := repo.ResolveProgram(ctx, collection.ID, 2, ""); !errors.Is(err, ErrCollectionProgramNotPending) {
		t.Fatalf("устаревшее решение после новой подачи: %v", err)
	}
	if err := repo.ResolveProgram(ctx, collection.ID, rejected.Revision+2, ""); err != nil {
		t.Fatal(err)
	}
	published, err := repo.GetByAuthorSlug(ctx, "author", "program")
	if err != nil || published.PublishedAt == nil || published.PublishIPHash != "ip" {
		t.Fatalf("публикация: %+v, %v", published, err)
	}
	if err := repo.SaveProgram(ctx, collection.ID, published.Revision, nil, nil); !errors.Is(err, ErrCollectionProgramPublished) {
		t.Fatalf("правка опубликованной программы: %v", err)
	}
	if err := repo.Unpublish(ctx, collection.ID); err != nil {
		t.Fatal(err)
	}
	removed, err := repo.GetByAuthorSlug(ctx, "author", "program")
	if err != nil || removed.PublishedAt != nil {
		t.Fatalf("снятие: %+v, %v", removed, err)
	}
	if err := repo.SaveProgram(ctx, collection.ID, removed.Revision, nil, nil); err != nil {
		t.Fatal(err)
	}
	if ok, _, err := repo.PublishWithinLimit(ctx, collection.ID, "ip", 3, since); err != nil || !ok {
		t.Fatalf("публикация плоской подборки: %v, %v", ok, err)
	}
}

func TestCollectionProgramSubmissionLimitSurvivesDeletion(t *testing.T) {
	ctx := context.Background()
	repo := NewCollectionRepository(testPool(t))
	owner := int64(1)
	since := time.Now().Add(-24 * time.Hour)
	for i, slug := range []string{"first", "second"} {
		c := &models.Collection{Title: "Программа", Slug: slug, AuthorNickname: "author", OwnerID: &owner}
		if err := repo.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		if err := repo.SaveProgram(ctx, c.ID, 0, []models.CollectionMeetingInput{{Title: "Встреча"}}, nil); err != nil {
			t.Fatal(err)
		}
		ok, err := repo.SubmitProgram(ctx, c.ID, 1, "shared", 1, since)
		if err != nil || ok != (i == 0) {
			t.Fatalf("подача %s: %v %v", slug, ok, err)
		}
		if err := repo.Delete(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
	}
}
