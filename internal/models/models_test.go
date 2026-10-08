package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestUserRoles(t *testing.T) {
	tests := []struct {
		role     UserRole
		expected string
	}{
		{RoleAdministrator, "administrator"},
		{RoleEditor, "editor"},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			if string(tt.role) != tt.expected {
				t.Errorf("Role = %s, want %s", tt.role, tt.expected)
			}
		})
	}
}

func TestPageStatus(t *testing.T) {
	tests := []struct {
		status   PageStatus
		expected string
	}{
		{PageStatusNotProofread, "не_вычитана"},
		{PageStatusInProgress, "вычитывается"},
		{PageStatusProofread, "вычитана"},
		{PageStatusHasIssues, "есть_проблемы"},
		{PageStatusEmpty, "пустая_страница"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("Status = %s, want %s", tt.status, tt.expected)
			}
		})
	}
}

func TestWorkStatus(t *testing.T) {
	tests := []struct {
		status   WorkStatus
		expected string
	}{
		{WorkStatusDraft, "draft"},
		{WorkStatusInProgress, "in_progress"},
		{WorkStatusCompleted, "completed"},
		{WorkStatusArchived, "archived"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("Status = %s, want %s", tt.status, tt.expected)
			}
		})
	}
}

func TestUserStruct(t *testing.T) {
	now := time.Now()
	user := User{
		ID:           1,
		Email:        "test@example.com",
		PasswordHash: "hashed_password",
		Role:         RoleEditor,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if user.ID != 1 {
		t.Errorf("ID = %d, want 1", user.ID)
	}
	if user.Email != "test@example.com" {
		t.Errorf("Email = %s, want test@example.com", user.Email)
	}
	if user.Role != RoleEditor {
		t.Errorf("Role = %s, want editor", user.Role)
	}
}

func TestWorkStruct(t *testing.T) {
	now := time.Now()
	pubDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	work := Work{
		ID:              1,
		Title:           "Test Work",
		Author:          "Test Author",
		PublicationDate: &pubDate,
		Language:        "ru",
		Country:         "Russia",
		FilePath:        "/path/to/file.pdf",
		Status:          WorkStatusDraft,
		OwnerID:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if work.ID != 1 {
		t.Errorf("ID = %d, want 1", work.ID)
	}
	if work.Title != "Test Work" {
		t.Errorf("Title = %s, want Test Work", work.Title)
	}
	if work.Status != WorkStatusDraft {
		t.Errorf("Status = %s, want draft", work.Status)
	}
}

func TestPageStruct(t *testing.T) {
	now := time.Now()
	chapterID := int64(5)
	page := Page{
		ID:              1,
		WorkID:          1,
		PageNumber:      42,
		PreviewPath:     "/uploads/preview.png",
		ContentMarkdown: "# Page Content",
		Status:          PageStatusNotProofread,
		ChapterID:       &chapterID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if page.PageNumber != 42 {
		t.Errorf("PageNumber = %d, want 42", page.PageNumber)
	}
	if page.Status != PageStatusNotProofread {
		t.Errorf("Status = %s, want not_proofread", page.Status)
	}
	if *page.ChapterID != 5 {
		t.Errorf("ChapterID = %d, want 5", *page.ChapterID)
	}
}

func TestPageVersion(t *testing.T) {
	now := time.Now()
	version := PageVersion{
		ID:              1,
		PageID:          1,
		ContentMarkdown: "Old content",
		VersionNumber:   3,
		UserID:          1,
		Comment:         "Fixed typo",
		CreatedAt:       now,
	}

	if version.VersionNumber != 3 {
		t.Errorf("VersionNumber = %d, want 3", version.VersionNumber)
	}
	if version.Comment != "Fixed typo" {
		t.Errorf("Comment = %s, want Fixed typo", version.Comment)
	}
}

func TestCategory(t *testing.T) {
	now := time.Now()
	category := Category{
		ID:          1,
		Name:        "Fiction",
		Slug:        "fiction",
		Description: "Fiction works",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if category.Name != "Fiction" {
		t.Errorf("Name = %s, want Fiction", category.Name)
	}
	if category.Slug != "fiction" {
		t.Errorf("Slug = %s, want fiction", category.Slug)
	}
}

func TestChapter(t *testing.T) {
	now := time.Now()
	chapter := Chapter{
		ID:          1,
		WorkID:      1,
		Title:       "Chapter 1",
		Type:        "chapter",
		OrderNumber: 1,
		StartPage:   1,
		EndPage:     10,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if chapter.Title != "Chapter 1" {
		t.Errorf("Title = %s, want Chapter 1", chapter.Title)
	}
	if chapter.StartPage != 1 {
		t.Errorf("StartPage = %d, want 1", chapter.StartPage)
	}
	if chapter.EndPage != 10 {
		t.Errorf("EndPage = %d, want 10", chapter.EndPage)
	}
}

func TestDocument(t *testing.T) {
	now := time.Now()
	ownerID := int64(1)
	doc := Document{
		ID:              1,
		Title:           "My Document",
		MarkdownContent: "# Hello\n\nWorld",
		OwnerID:         &ownerID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if doc.Title != "My Document" {
		t.Errorf("Title = %s, want My Document", doc.Title)
	}
	if doc.MarkdownContent != "# Hello\n\nWorld" {
		t.Errorf("MarkdownContent mismatch")
	}
}

func TestFootnote(t *testing.T) {
	now := time.Now()
	fn := Footnote{
		ID:              1,
		PageID:          1,
		ContentMarkdown: "This is a footnote",
		OrderNumber:     1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if fn.ContentMarkdown != "This is a footnote" {
		t.Errorf("ContentMarkdown = %s, want This is a footnote", fn.ContentMarkdown)
	}
}

func TestStatusHistory(t *testing.T) {
	now := time.Now()
	sh := StatusHistory{
		ID:         1,
		EntityType: "page",
		EntityID:   1,
		Status:     "proofread",
		UserID:     1,
		CreatedAt:  now,
	}

	if sh.EntityType != "page" {
		t.Errorf("EntityType = %s, want page", sh.EntityType)
	}
	if sh.Status != "proofread" {
		t.Errorf("Status = %s, want proofread", sh.Status)
	}
}

func TestNilPointers(t *testing.T) {
	work := Work{}
	if work.PublicationDate != nil {
		t.Error("Expected nil PublicationDate")
	}

	page := Page{}
	if page.ChapterID != nil {
		t.Error("Expected nil ChapterID")
	}
}

func TestCollectionEntryResolvedAuthor(t *testing.T) {
	// Переопределение перебивает автора работы: глава Энгельса внутри тома
	// МиЭ иначе подписывается авторством тома целиком.
	e := &CollectionEntry{WorkAuthor: "К. Маркс, Ф. Энгельс", AuthorOverride: "Ф. Энгельс"}
	if got := e.ResolvedAuthor(); got != "Ф. Энгельс" {
		t.Fatalf("автор %q, ожидался %q", got, "Ф. Энгельс")
	}

	// Пустое переопределение означает «брать автора работы», а не «автора нет».
	e = &CollectionEntry{WorkAuthor: "Э. В. Ильенков", AuthorOverride: ""}
	if got := e.ResolvedAuthor(); got != "Э. В. Ильенков" {
		t.Fatalf("автор %q, ожидался %q", got, "Э. В. Ильенков")
	}
}

// DocumentCut встраивает Anchor безымянным полем — наружу обязан ходить
// номер полосы, а не pages.id (решение о грамматике адреса), и смещения/
// хэши/цитаты читателю тоже не нужны. Тег json:"-" стоит на самом Anchor,
// поэтому проверка держит контракт типа, а не одного вызывающего места:
// document_cut_handler_test.go повторяет ту же проверку уже на реальном
// HTTP-ответе Create.
//
// Список наружу — БЕЛЫЙ, а не чёрный: чёрный список по именам Go-полей
// (I2, найдено мутацией) ловит только полное снятие json:"-" — если у поля
// завести собственный тег json:"start_page_id" вместо "-" (соглашение
// соседнего IndexFragment), оно перестаёт называться "StartPageID" в JSON и
// проходит проверку "по имени поля", хотя утечка ровно та же. Белый список
// требует, чтобы множество ключей ответа было РОВНО ожидаемым — новое поле
// с любым именем меняет множество и роняет тест.
func TestDocumentCutJSONHidesPageInternals(t *testing.T) {
	cut := DocumentCut{
		ID:         1,
		DocumentID: 2,
		Anchor: Anchor{
			StartPageID: 999888,
			StartOffset: 10,
			EndPageID:   999889,
			EndOffset:   20,
			HeadQuote:   "начало",
			TailQuote:   "конец",
			StartHash:   "abc",
			EndHash:     "def",
		},
		Status:      CutStatusOK,
		SourceTitle: "источник",
	}

	body, err := json.Marshal(cut)
	if err != nil {
		t.Fatalf("сериализация: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("разбор JSON: %v", err)
	}

	wantKeys := map[string]bool{
		"id": true, "document_id": true, "work_id": true,
		"status": true, "source_title": true, "created_at": true,
	}
	for key := range raw {
		if !wantKeys[key] {
			t.Fatalf("неожиданный ключ %q в JSON (внутреннее поле вклейки утекло наружу): %s", key, body)
		}
	}
	for key := range wantKeys {
		if _, ok := raw[key]; !ok {
			t.Fatalf("ожидаемый ключ %q пропал из JSON: %s", key, body)
		}
	}
	// Значение pages.id (999888/999889) не должно всплыть под ЛЮБЫМ именем.
	if strings.Contains(string(body), "999888") || strings.Contains(string(body), "999889") {
		t.Fatalf("pages.id вклейки утёк в JSON под каким-то именем: %s", body)
	}
}

func TestValidDocumentRejectReason(t *testing.T) {
	for _, r := range []DocumentRejectReason{
		DocumentRejectOffTopic,
		DocumentRejectNoCommentary,
		DocumentRejectAbuse,
		DocumentRejectUnlawful,
	} {
		if !ValidDocumentRejectReason(r) {
			t.Errorf("причина %q обязана входить в список", r)
		}
	}
	// Причина отказа предложению правки — НЕ причина отказа разбору: списки
	// разные, и перепутать их значит показать автору разбора «так в оригинале».
	if ValidDocumentRejectReason(DocumentRejectReason("так_в_оригинале")) {
		t.Error("чужая причина принята как своя")
	}
	if ValidDocumentRejectReason("") {
		t.Error("пустая причина принята")
	}
}
