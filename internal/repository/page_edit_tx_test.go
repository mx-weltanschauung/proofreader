package repository

import (
	"context"
	"testing"

	"proofreader/internal/models"
)

// seedPageForEdit кладёт в базу работу и одну полосу настоящими
// производителями — WorkRepository.Create и PageRepository.Create, — а не
// сырым INSERT: строка обязана быть той же формы, какую видит боевой код.
func seedPageForEdit(t *testing.T, text string) (*PageRepository, *PageVersionRepository, *models.Page) {
	t.Helper()
	ctx := context.Background()
	pool := testPool(t)

	works := NewWorkRepository(pool)
	vol := newVolume(t, works, "проба-атомарной-правки")

	pages := NewPageRepository(pool)
	page := &models.Page{
		WorkID: vol.ID, PageNumber: 1, ContentMarkdown: text,
		Status: models.PageStatusNotProofread,
	}
	if err := pages.Create(ctx, page); err != nil {
		t.Fatalf("create page: %v", err)
	}
	return pages, NewPageVersionRepository(pool), page
}

func countVersions(t *testing.T, pages *PageRepository, pageID int64) int {
	t.Helper()
	var n int
	if err := pages.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM page_versions WHERE page_id = $1`, pageID).Scan(&n); err != nil {
		t.Fatalf("count versions: %v", err)
	}
	return n
}

// Провалившаяся правка не должна оставлять снимок.
//
// Найдено на живом боевом: PUT /pages/{id} без поля status отвечал 500
// («invalid input value for enum page_status: ""»), текст полосы честно не
// менялся — а строка в page_versions от этого запроса оставалась. По
// содержимому она байт в байт повторяла прежний текст, то есть история
// отмечала правку, которой не было. Снимок и запись шли двумя раздельными
// действиями, и откатывать первое было нечем.
func TestPageRepository_SaveEditRollsBackVersionWhenPageWriteFails(t *testing.T) {
	ctx := context.Background()
	pages, _, page := seedPageForEdit(t, "исходный текст")
	before := countVersions(t, pages, page.ID)

	snapshot := &models.PageVersion{
		PageID:          page.ID,
		ContentMarkdown: page.ContentMarkdown,
		VersionNumber:   1,
		UserID:          1,
		Comment:         "правка, которой не будет",
	}
	// Пустой статус — ровно то, чем запрос падал на боевом: enum page_status
	// пустой строки не знает, и UPDATE отбивается уже Postgres'ом.
	page.ContentMarkdown = "новый текст"
	page.Status = ""

	if err := pages.SaveEdit(ctx, page, snapshot); err == nil {
		t.Fatal("SaveEdit с пустым статусом обязан вернуть ошибку")
	}

	if after := countVersions(t, pages, page.ID); after != before {
		t.Errorf("провалившаяся правка оставила %d снимков вместо %d: транзакции нет",
			after, before)
	}
	fresh, err := pages.GetByID(ctx, page.ID)
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	if fresh.ContentMarkdown != "исходный текст" {
		t.Errorf("текст полосы %q, ожидался прежний", fresh.ContentMarkdown)
	}
}

// Удачная правка пишет обе строки и возвращает вызывающему то, что проставила
// база: id и время снимка, время правки полосы. Без этой половины откат можно
// было бы «обеспечить», не записывая вовсе ничего.
func TestPageRepository_SaveEditWritesBothRows(t *testing.T) {
	ctx := context.Background()
	pages, versions, page := seedPageForEdit(t, "было")

	snapshot := &models.PageVersion{
		PageID:          page.ID,
		ContentMarkdown: page.ContentMarkdown,
		VersionNumber:   1,
		UserID:          1,
		Comment:         "правка редактора",
	}
	wasUpdated := page.UpdatedAt
	page.ContentMarkdown = "стало"
	page.Status = models.PageStatusProofread

	if err := pages.SaveEdit(ctx, page, snapshot); err != nil {
		t.Fatalf("SaveEdit: %v", err)
	}

	if snapshot.ID == 0 || snapshot.CreatedAt.IsZero() {
		t.Errorf("снимок вернулся без id/времени: %+v", snapshot)
	}
	if !page.UpdatedAt.After(wasUpdated) {
		t.Errorf("updated_at полосы не сдвинулся: %v -> %v", wasUpdated, page.UpdatedAt)
	}

	fresh, err := pages.GetByID(ctx, page.ID)
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	if fresh.ContentMarkdown != "стало" || fresh.Status != models.PageStatusProofread {
		t.Errorf("полоса в базе: %q / %q", fresh.ContentMarkdown, fresh.Status)
	}

	stored, err := versions.ListByPage(ctx, page.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("снимков в базе %d, ожидался 1", len(stored))
	}
	// В снимке — текст ДО правки: история хранит то, что заменили.
	if stored[0].ContentMarkdown != "было" {
		t.Errorf("в снимке текст %q, ожидался прежний", stored[0].ContentMarkdown)
	}
}

// Правка текста метит устаревшими только дорожки, накрывающие полосу; правка
// одного статуса (текст тот же) — не метит ничего.
func TestSaveEditMarksOnlyCoveringAudioStale(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := seedWorkWithPages(t, pool, "том", "раз", "два", "три")
	audioRepo := NewAudioRepository(pool)
	// Три дорожки по одной полосе: соседи слева и справа ловят обе границы
	// условия BETWEEN.
	early := trackFor(w, []string{"раз"}, 1, 1)
	cover := trackFor(w, []string{"два"}, 2, 2)
	other := trackFor(w, []string{"три"}, 3, 3)
	if _, _, err := audioRepo.RegisterTracks(ctx, w, []models.AudioTrack{early, cover, other}); err != nil {
		t.Fatal(err)
	}
	pages := NewPageRepository(pool)
	page, err := pages.GetByWorkAndPageNumber(ctx, w, 2)
	if err != nil {
		t.Fatal(err)
	}
	stale := func() map[int]bool {
		list, _ := audioRepo.ListTracks(ctx, w)
		m := map[int]bool{}
		for _, tr := range list {
			m[tr.StartPage] = tr.Stale
		}
		return m
	}

	// Только статус.
	page.Status = models.PageStatusProofread
	if err := pages.SaveEdit(ctx, page, &models.PageVersion{
		PageID: page.ID, ContentMarkdown: page.ContentMarkdown, VersionNumber: 1, UserID: 1}); err != nil {
		t.Fatal(err)
	}
	if s := stale(); s[1] || s[2] || s[3] {
		t.Fatalf("правка статуса пометила звук: %v", s)
	}

	// Текст.
	prev := page.ContentMarkdown
	page.ContentMarkdown = "два, поправлено"
	if err := pages.SaveEdit(ctx, page, &models.PageVersion{
		PageID: page.ID, ContentMarkdown: prev, VersionNumber: 2, UserID: 1}); err != nil {
		t.Fatal(err)
	}
	if s := stale(); s[1] || !s[2] || s[3] {
		t.Errorf("после правки текста: %v, ждали {1:false, 2:true, 3:false}", s)
	}
}

// Номера полос у каждого тома свои: правка полосы 2 одного тома не старит
// дорожку другого тома на тех же номерах.
func TestSaveEditStalesOnlyOwnWorkAudio(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	w := seedWorkWithPages(t, pool, "том", "раз", "два")
	other := seedWorkWithPages(t, pool, "чужой том", "раз", "два")
	audioRepo := NewAudioRepository(pool)
	if _, _, err := audioRepo.RegisterTracks(ctx, w, []models.AudioTrack{trackFor(w, []string{"раз", "два"}, 1, 2)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := audioRepo.RegisterTracks(ctx, other, []models.AudioTrack{trackFor(other, []string{"раз", "два"}, 1, 2)}); err != nil {
		t.Fatal(err)
	}
	pages := NewPageRepository(pool)
	page, err := pages.GetByWorkAndPageNumber(ctx, w, 2)
	if err != nil {
		t.Fatal(err)
	}
	prev := page.ContentMarkdown
	page.ContentMarkdown = "два, поправлено"
	if err := pages.SaveEdit(ctx, page, &models.PageVersion{
		PageID: page.ID, ContentMarkdown: prev, VersionNumber: 1, UserID: 1}); err != nil {
		t.Fatal(err)
	}
	own, _ := audioRepo.ListTracks(ctx, w)
	foreign, _ := audioRepo.ListTracks(ctx, other)
	if len(own) != 1 || !own[0].Stale {
		t.Errorf("своя дорожка не устарела: %+v", own)
	}
	if len(foreign) != 1 || foreign[0].Stale {
		t.Errorf("дорожка чужого тома устарела от чужой правки: %+v", foreign)
	}
}
