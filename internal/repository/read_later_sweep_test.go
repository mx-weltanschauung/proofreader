package repository_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/repository"
)

func TestReadLaterShelfAcrossSessions(t *testing.T) {
	pool := repository.SharedTestPool(t)
	var f *closedSweep
	router, _, _ := closedSweepRouter(t, pool, func(_ *config.Config, authService *auth.Service) {
		f = seedClosedSweep(t, pool, authService)
	})
	const shelf = "/api/read-later"
	request := func(method, path, token string, code int) string {
		t.Helper()
		res := sweepSend(t, router, method, path, token, "")
		if res.code != code {
			t.Errorf("%s %s: HTTP %d, want %d, %s", method, path, res.code, code, res.text)
		}
		return res.text
	}
	type entry struct {
		WorkID    int64  `json:"work_id"`
		ChapterID int64  `json:"chapter_id"`
		Title     string `json:"title"`
		Source    string `json:"source"`
	}
	list := func(token string) []entry {
		t.Helper()
		var entries []entry
		if err := json.Unmarshal([]byte(request(http.MethodGet, shelf, token, http.StatusOK)), &entries); err != nil {
			t.Fatalf("decode shelf: %v", err)
		}
		return entries
	}
	volume := shelf + "/work/" + itoa(f.ch.work)
	chapter := shelf + "/chapter/" + itoa(f.ch.grandchild)
	article := shelf + "/chapter/" + itoa(f.ch.article)

	request(http.MethodGet, shelf, "", http.StatusUnauthorized)
	request(http.MethodPut, volume, "", http.StatusUnauthorized)
	request(http.MethodDelete, volume, "", http.StatusUnauthorized)
	request(http.MethodPut, shelf+"/work/0", f.readerTok, http.StatusBadRequest)
	request(http.MethodPut, shelf+"/other/1", f.readerTok, http.StatusBadRequest)
	request(http.MethodPut, shelf+"/work/"+itoa(f.edVol), f.readerTok, http.StatusNotFound)

	articleClosure := "/api/works/" + itoa(f.ch.issue) + "/chapters/" + itoa(f.ch.article) + "/closed"
	if res := sweepSend(t, router, http.MethodPut, articleClosure, f.adminTok, `{"closed":false,"reason":"Приёмка"}`); res.code != http.StatusOK {
		t.Fatalf("open article: %+v", res)
	}
	request(http.MethodPut, volume, f.readerTok, http.StatusNoContent)
	request(http.MethodPut, chapter, f.readerTok, http.StatusNoContent)
	var add sync.WaitGroup
	for range 6 {
		add.Go(func() { request(http.MethodPut, article, f.readerTok, http.StatusNoContent) })
	}
	add.Wait()
	request(http.MethodPut, volume, f.readerTok, http.StatusNoContent)
	request(http.MethodDelete, article, f.editorTok, http.StatusNoContent)
	if got := list(f.editorTok); len(got) != 0 {
		t.Fatalf("second account read another shelf: %+v", got)
	}
	got := list(f.readerTok)
	if len(got) != 3 || got[0].ChapterID != f.ch.article || got[0].WorkID != f.ch.issue ||
		got[1].ChapterID != f.ch.grandchild || got[2].WorkID != f.ch.work || got[2].ChapterID != 0 ||
		got[0].Title == "" || !strings.Contains(got[0].Source, "№") {
		t.Fatalf("unexpected shelf: %+v", got)
	}

	if _, err := pool.Exec(context.Background(), "UPDATE chapters SET scans_hidden=true WHERE id=$1", f.ch.article); err != nil {
		t.Fatal(err)
	}
	if got := list(f.readerTok); len(got) != 3 {
		t.Fatalf("hiding scans removed an open text: %+v", got)
	}
	for _, path := range []string{
		"/api/works/" + itoa(f.ch.work) + "/chapters/" + itoa(f.ch.closed) + "/closed",
		articleClosure,
	} {
		if res := sweepSend(t, router, http.MethodPut, path, f.adminTok, `{"closed":true,"reason":"Приёмка"}`); res.code != http.StatusOK {
			t.Fatalf("close source: %+v", res)
		}
	}
	for _, path := range []string{chapter, article} {
		request(http.MethodPut, path, f.readerTok, http.StatusNotFound)
	}
	if got := list(f.readerTok); len(got) != 1 || got[0].WorkID != f.ch.work || got[0].ChapterID != 0 {
		t.Fatalf("closed publications in shelf: %+v", got)
	}
	request(http.MethodPut, article, f.editorTok, http.StatusNoContent)
	if got := list(f.editorTok); len(got) != 1 || got[0].ChapterID != f.ch.article {
		t.Fatalf("staff cannot save closed article: %+v", got)
	}
	request(http.MethodDelete, chapter, f.readerTok, http.StatusNoContent)
	request(http.MethodDelete, volume, f.readerTok, http.StatusNoContent)
	if got := list(f.readerTok); len(got) != 0 {
		t.Fatalf("removed volume remains: %+v", got)
	}

	// Удаление открытой главы убирает ссылку, а не скрывает её до повторного открытия.
	neighbour := shelf + "/chapter/" + itoa(f.ch.neighbour)
	request(http.MethodPut, neighbour, f.readerTok, http.StatusNoContent)
	request(http.MethodDelete, "/api/works/"+itoa(f.ch.work)+"/chapters/"+itoa(f.ch.neighbour), f.adminTok, http.StatusNoContent)
	if got := list(f.readerTok); len(got) != 0 {
		t.Fatalf("deleted source remains: %+v", got)
	}
}
