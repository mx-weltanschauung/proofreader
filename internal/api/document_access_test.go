package api

import (
	"net/http"
	"testing"
	"time"

	"proofreader/internal/auth"
	"proofreader/internal/models"
)

func readerClaims(id int64, nick string) *auth.Claims {
	return &auth.Claims{UserID: id, Role: models.RoleReader, Nickname: nick}
}

func editorClaims() *auth.Claims {
	return &auth.Claims{UserID: 100, Role: models.RoleEditor}
}

// ptrInt64 живёт в work_volume_test.go (та же сигнатура и семантика) — не
// заводится здесь повторно.

func timePtr(t time.Time) *time.Time { return &t }

func readerDoc(ownerID int64, nick string) *models.Document {
	return &models.Document{ID: 7, OwnerID: ptrInt64(ownerID), AuthorNickname: nick,
		ReviewStatus: models.DocumentDraft}
}

func TestMayEditDocumentFollowsNicknameSnapshot(t *testing.T) {
	d := readerDoc(5, "чтец")
	if !mayEditDocument(readerClaims(5, "чтец"), d) {
		t.Error("владелец не допущен к своему разбору")
	}
	if mayEditDocument(readerClaims(6, "другой"), d) {
		t.Error("посторонний читатель допущен к чужому разбору")
	}
	// Редактор чужой (читательский) разбор НЕ правит: «мы размещаем, автор
	// собирает» — то же разделение, что у подборок.
	if mayEditDocument(editorClaims(), d) {
		t.Error("редактор допущен к правке читательского разбора")
	}
	// Осиротевший разбор: учётку удалили (ON DELETE SET NULL), подпись живёт
	// снимком. Он не достаётся НИКОМУ — ни бывшему владельцу, ни редактору.
	orphan := &models.Document{ID: 8, OwnerID: nil, AuthorNickname: "чтец"}
	if mayEditDocument(readerClaims(5, "чтец"), orphan) {
		t.Error("осиротевший разбор достался бывшему владельцу")
	}
	if mayEditDocument(editorClaims(), orphan) {
		t.Error("осиротевший разбор достался редактору «по доброте»")
	}
	// Сотруднический разбор (подписи нет) правит любой сотрудник.
	staffDoc := &models.Document{ID: 9, AuthorNickname: ""}
	if !mayEditDocument(editorClaims(), staffDoc) {
		t.Error("редактор не допущен к сотрудническому разбору")
	}
	if mayEditDocument(readerClaims(5, "чтец"), staffDoc) {
		t.Error("читатель допущен к сотрудническому разбору")
	}
	if mayEditDocument(nil, staffDoc) {
		t.Error("аноним допущен к правке")
	}
}

func TestModeratorReadsSubmittedDraftOnly(t *testing.T) {
	// Неотправленный черновик приватен и от редактора тоже.
	draft := readerDoc(5, "чтец")
	if mayReadDraft(editorClaims(), draft) {
		t.Error("редактор читает неотправленный черновик читателя")
	}
	// Поданное на рассмотрение он читает целиком — судить иначе не о чем.
	pending := readerDoc(5, "чтец")
	pending.ReviewStatus = models.DocumentPending
	if !mayReadDraft(editorClaims(), pending) {
		t.Error("редактор не видит того, что ему подали")
	}
	if mayReadDraft(readerClaims(6, "другой"), pending) {
		t.Error("посторонний читатель видит чужой черновик на рассмотрении")
	}
	if !mayReadDraft(readerClaims(5, "чтец"), draft) {
		t.Error("автор не видит собственного черновика")
	}
}

func TestDocumentVisibilityDistinguishesNeverFromTaken(t *testing.T) {
	published := readerDoc(5, "чтец")
	published.PublishedAt = timePtr(time.Now())
	published.WasPublished = true
	if _, _, ok := documentVisibleTo(nil, published); !ok {
		t.Error("опубликованный разбор не виден анониму")
	}

	never := readerDoc(5, "чтец")
	status, _, ok := documentVisibleTo(nil, never)
	if ok || status != http.StatusNotFound {
		t.Errorf("черновик постороннему: ожидался 404, получено %d ok=%v", status, ok)
	}

	taken := readerDoc(5, "чтец")
	taken.WasPublished = true
	status, _, ok = documentVisibleTo(nil, taken)
	if ok || status != http.StatusGone {
		t.Errorf("снятый разбор: ожидался 410, получено %d ok=%v", status, ok)
	}
}

func TestDocumentForViewerHidesPendingRevision(t *testing.T) {
	d := readerDoc(5, "чтец")
	d.Title = "Новое заглавие"
	d.MarkdownContent = "новое тело, ещё не одобрено"
	d.PublishedTitle = "Старое заглавие"
	d.PublishedMarkdown = "старое тело, одобрено"
	d.PublishedAt = timePtr(time.Now())
	d.WasPublished = true
	d.ReviewStatus = models.DocumentPending
	reason := models.DocumentRejectAbuse
	d.RejectReason = &reason

	got := documentForViewer(nil, d)
	if got.MarkdownContent != "старое тело, одобрено" {
		t.Errorf("постороннему уехал неодобренный черновик: %q", got.MarkdownContent)
	}
	if got.Title != "Старое заглавие" {
		t.Errorf("постороннему уехало неодобренное заглавие: %q", got.Title)
	}
	if got.ReviewStatus != "" || got.RejectReason != nil {
		t.Error("постороннему уехала кухня модерации")
	}
	// Исходная строка не тронута: подмена обязана быть копией, иначе
	// следующий читатель получит уже испорченный объект.
	if d.MarkdownContent != "новое тело, ещё не одобрено" {
		t.Error("documentForViewer испортил исходный разбор")
	}
	// Автору — как есть.
	mine := documentForViewer(readerClaims(5, "чтец"), d)
	if mine.MarkdownContent != "новое тело, ещё не одобрено" {
		t.Error("автор не видит собственного черновика")
	}
}

func TestDocumentForViewerShowsSubmittedDraftToModerator(t *testing.T) {
	d := readerDoc(5, "чтец")
	d.MarkdownContent = "поданное тело"
	d.PublishedMarkdown = "прежнее тело"
	d.ReviewStatus = models.DocumentPending
	got := documentForViewer(editorClaims(), d)
	if got.MarkdownContent != "поданное тело" {
		t.Fatalf("модератор судит не о том, что ему подали: %q", got.MarkdownContent)
	}
}
