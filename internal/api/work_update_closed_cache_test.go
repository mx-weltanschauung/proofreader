package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/pagecache"
)

// movingWorkRepo — работа, чья действующая закрытость меняется записью:
// первое чтение отдаёт причину до правки, следующие — после.
type movingWorkRepo struct {
	fakeWorkRepo
	before, after string
	reads         int
}

func (r *movingWorkRepo) GetByID(_ context.Context, id int64) (*models.Work, error) {
	r.reads++
	by := r.before
	if r.reads > 1 {
		by = r.after
	}
	return &models.Work{ID: id, Role: models.WorkRoleVolume, ClosedBy: by}, nil
}

// PUT /works/{id} пишет edition_id и parent_work_id, а они меняют действующую
// закрытость. Без сброса /seo, .md, MCP и карточки отдавали бы текст и
// заглавие тома, ставшего закрытым, до часа (карточки — до суток), а том,
// ставший открытым, — закэшированным отказом.
func TestUpdateWorkDropsCacheWhenClosednessChanges(t *testing.T) {
	cases := []struct {
		name          string
		before, after string
		wantDrop      bool
	}{
		{"перенос в закрытое издание закрывает", "", models.ClosedByEdition, true},
		{"перенос из закрытого издания открывает", models.ClosedByEdition, "", true},
		{"закрытость не изменилась", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache := pagecache.New(t.TempDir())
			own := pagecache.Key{WorkID: 4, Start: 1, End: 10}
			child := pagecache.Key{WorkID: 41, Start: 1, End: 3}
			for _, k := range []pagecache.Key{own, child} {
				if err := cache.Put(k, []byte(`{"pages":[]}`)); err != nil {
					t.Fatalf("Put %+v: %v", k, err)
				}
			}
			crawler := &fakeCrawlerCaches{}
			repo := &movingWorkRepo{before: tc.before, after: tc.after}
			repo.children = map[int64][]*models.Work{4: {{ID: 41, Role: models.WorkRoleFrontMatter}}}
			h := NewWorkHandler(repo, nil, nil, nil, 0, NewServingCache(cache, crawler), nil)

			req := httptest.NewRequest(http.MethodPut, "/api/works/4",
				bytes.NewBufferString(`{"title":"Т","status":"draft","edition_id":5}`))
			rec := httptest.NewRecorder()
			h.Update(rec, mux.SetURLVars(req, map[string]string{"id": "4"}))
			if rec.Code != http.StatusOK {
				t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
			}
			_, _, ownKept := cache.Open(own)
			_, _, childKept := cache.Open(child)
			if tc.wantDrop {
				if ownKept || childKept || crawler.calls == 0 {
					t.Errorf("кэш не сброшен: том %v, ребёнок %v, краулер %d", ownKept, childKept, crawler.calls)
				}
			} else if !ownKept || !childKept || crawler.calls != 0 {
				t.Errorf("кэш сброшен без смены закрытости: том %v, ребёнок %v, краулер %d", ownKept, childKept, crawler.calls)
			}
		})
	}
}
