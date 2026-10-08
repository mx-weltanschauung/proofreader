package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"proofreader/internal/limit"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

func strp(s string) *string { return &s }
func i64p(v int64) *int64   { return &v }

func leninResult() *models.SearchResult {
	return &models.SearchResult{
		Query: "кооперация", Terms: []string{"кооперац"}, TotalHits: 63,
		Concepts: []models.SearchConcept{{Slug: "kooperaciya", Title: "Кооперация"}},
		Chapters: []models.SearchChapter{{ID: 900, Title: "О кооперации", Slug: "o-kooperacii",
			WorkID: 118, WorkSlug: "lenin-t45", WorkTitle: "Том 45", VolumeLabel: "т. 45",
			EditionTitle: "ПСС Ленина"}},
		Volumes: []models.SearchVolume{
			{WorkID: 170, WorkSlug: "lenin-t03", Title: "Том 3", VolumeLabel: "т. 3", EditionTitle: "ПСС Ленина", TextHits: 27,
				Pages: []models.SearchPage{{PageNumber: 12, PrintedNumber: 12, Snippet: "о \x01кооперации\x02 крестьян"}}},
			{WorkID: 114, WorkSlug: "lenin-t43", Title: "Том 43", VolumeLabel: "т. 43", EditionTitle: "ПСС Ленина", TextHits: 36, ApparatusHits: 22,
				Pages: []models.SearchPage{
					{PageNumber: 4, PrintedNumber: 4, ChapterTitle: strp("1. Речь"), ChapterID: i64p(10807), Snippet: "Отношение к \x01кооперации\x02"},
					{PageNumber: 590, PrintedNumber: 590, IsApparatus: true, Snippet: "примечание о \x01кооперации\x02"},
				}},
		},
	}
}

func TestSearchCompactsResult(t *testing.T) {
	st := &fakeSearch{result: leninResult()}
	s := newService(Deps{BaseURL: base, Search: st, SearchSlots: limit.New(2)}, testGates())
	out, err := s.search(context.Background(), SearchInput{Query: "  кооперация ", Editions: []int64{4}, Works: []int64{114}})
	if err != nil {
		t.Fatal(err)
	}
	if st.gotQuery.Text != "кооперация" || len(st.gotQuery.EditionIDs) != 1 || st.gotQuery.WorkIDs[0] != 114 {
		t.Errorf("в хранилище ушло %+v", st.gotQuery)
	}
	kinds := []string{}
	for _, h := range out.Results {
		kinds = append(kinds, h.Kind)
		if !strings.HasPrefix(h.URL, base+"/") || h.URL != base+h.ID || h.Title == "" {
			t.Errorf("результат без адреса или заглавия: %+v", h)
		}
	}
	if got := strings.Join(kinds, ","); got != "concept,chapter,page,page,page" {
		t.Errorf("порядок %s: понятия, главы, потом полосы", got)
	}
	// Тома по text_hits: 43-й (36) раньше 3-го (27), хотя пришёл вторым.
	if out.Results[2].ID != "/works/114-lenin-t43/pages/4" {
		t.Errorf("первая полоса %q, ожидалась полоса тома с наибольшим числом совпадений", out.Results[2].ID)
	}
	if out.Results[2].Snippet != "Отношение к **кооперации**" || out.Results[2].PrintedPage != 4 || out.Results[2].Chapter != "1. Речь" {
		t.Errorf("полоса: %+v", out.Results[2])
	}
	if !out.Results[3].Apparatus || !strings.Contains(out.Results[3].Title, "примечания редакции, не текст автора") {
		t.Errorf("полоса аппарата не помечена: %+v", out.Results[3])
	}
	if out.Results[0].ID != "/concepts/kooperaciya" || out.Results[1].ID != "/works/118-lenin-t45/chapters/900-o-kooperacii" {
		t.Errorf("понятие/глава: %+v / %+v", out.Results[0], out.Results[1])
	}
	if !strings.Contains(out.Summary, "63") || !strings.Contains(out.Summary, "т. 43") {
		t.Errorf("сводка: %s", out.Summary)
	}
}

// Каталог в 550 КБ ужимается до бюджета, и видно, что показано не всё.
func TestSearchFitsBudget(t *testing.T) {
	res := &models.SearchResult{Query: "класс", Terms: []string{"класс"}, TotalHits: 40000}
	long := strings.Repeat("слово \x01класс\x02 ", 150)
	for i := 0; i < 127; i++ {
		v := models.SearchVolume{WorkID: int64(i + 1), WorkSlug: fmt.Sprintf("t%d", i), Title: fmt.Sprintf("Том %d", i),
			VolumeLabel: fmt.Sprintf("т. %d", i), EditionTitle: "Собрание", TextHits: (i * 37) % 500}
		for p := 0; p < 3; p++ {
			v.Pages = append(v.Pages, models.SearchPage{PageNumber: p + 1, PrintedNumber: p + 1, Snippet: long})
		}
		res.Volumes = append(res.Volumes, v)
	}
	for i := 0; i < 20; i++ {
		res.Chapters = append(res.Chapters, models.SearchChapter{ID: int64(i + 1), Title: strings.Repeat("Глава ", 10), WorkID: 1, WorkSlug: "t0"})
		res.Concepts = append(res.Concepts, models.SearchConcept{Slug: fmt.Sprintf("c%d", i), Title: "Понятие"})
	}
	s := newService(Deps{BaseURL: base, Search: &fakeSearch{result: res}, SearchSlots: limit.New(2)}, testGates())
	out, err := s.search(context.Background(), SearchInput{Query: "класс"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if n := utf8.RuneCount(raw); n > searchBudget {
		t.Errorf("ответ %d знаков, бюджет %d", n, searchBudget)
	}
	var pages, chapters, concepts int
	for _, h := range out.Results {
		switch h.Kind {
		case "page":
			pages++
			if utf8.RuneCountInString(h.Snippet) > snippetRunes+3 {
				t.Errorf("отрывок %d знаков", utf8.RuneCountInString(h.Snippet))
			}
		case "chapter":
			chapters++
		case "concept":
			concepts++
		}
	}
	if pages == 0 || chapters > maxChapters || concepts > maxConcepts {
		t.Errorf("полос %d, глав %d, понятий %d", pages, chapters, concepts)
	}
	if !strings.Contains(out.Summary, "Ещё") {
		t.Errorf("сводка не говорит, что показано не всё: %s", out.Summary)
	}
}

func TestSnippetBoldsAndCutsBalanced(t *testing.T) {
	got := snippet("a \x01b\x02 c")
	if got != "a **b** c" {
		t.Errorf("%q", got)
	}
	long := strings.Repeat("я", snippetRunes-3) + " \x01слово\x02 хвост"
	got = snippet(long)
	if strings.Count(got, "**")%2 != 0 {
		t.Errorf("разрезанное выделение не закрыто: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("обрезка без многоточия: %q", got)
	}
}

// Review Focus 3.
func TestSearchRejectsShortAndEmpty(t *testing.T) {
	st := &fakeSearch{result: &models.SearchResult{Terms: []string{}}}
	s := newService(Deps{BaseURL: base, Search: st, SearchSlots: limit.New(2)}, testGates())
	for q, want := range map[string]string{"и": models.SearchTooShortMessage, "   ": msgEmpty, "-кооперация": msgEmpty} {
		_, err := s.search(context.Background(), SearchInput{Query: q})
		if err == nil || userError(err).Error() != want {
			t.Errorf("%q: %v, ожидалось %q", q, err, want)
		}
	}
}

func TestSearchTimeoutSaysNarrowDown(t *testing.T) {
	st := &fakeSearch{err: fmt.Errorf("поиск: %w", repository.ErrSearchTimeout)}
	s := newService(Deps{BaseURL: base, Search: st, SearchSlots: limit.New(2)}, testGates())
	_, err := s.search(context.Background(), SearchInput{Query: "класс"})
	if userError(err).Error() != msgTimeout {
		t.Errorf("%v", err)
	}
}

// У search_volume нет works/editions: совет сузить область ему не годится.
func TestSearchVolumeTimeoutSaysNarrowByChapters(t *testing.T) {
	st := &fakeSearch{err: fmt.Errorf("поиск: %w", repository.ErrSearchTimeout)}
	works := fakeWorks{114: {ID: 114, Slug: "lenin-t43", Title: "Том 43"}}
	s := newService(Deps{BaseURL: base, Search: st, SearchSlots: limit.New(2), Works: works}, testGates())
	_, err := s.searchVolume(context.Background(), VolumeSearchInput{Query: "класс", Work: 114})
	if got := userError(err).Error(); got != msgTimeoutVolume || !strings.Contains(got, "chapters") || strings.Contains(got, "works") {
		t.Errorf("%q", got)
	}
}

// Главное: MCP держит не больше одного из общих слотов, второй — сайту.
func TestSearchHoldsAtMostOneSharedSlot(t *testing.T) {
	shared := limit.New(2)
	st := &fakeSearch{result: leninResult(), block: make(chan struct{}), entered: make(chan struct{}, 10)}
	s := newService(Deps{BaseURL: base, Search: st, SearchSlots: shared}, testGates())
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			_, err := s.search(context.Background(), SearchInput{Query: "кооперация"})
			errs <- err
		}()
	}
	<-st.entered
	time.Sleep(30 * time.Millisecond) // остальные двое вошли бы, если бы могли
	if n := len(shared); n != 1 {
		t.Fatalf("MCP держит %d общих слотов, ожидался 1", n)
	}
	rel, ok := shared.TryAcquire()
	if !ok {
		t.Fatal("читателю сайта не осталось слота")
	}
	rel()
	close(st.block)
	for i := 0; i < 3; i++ {
		if err := <-errs; err != nil {
			t.Errorf("поиск %d: %v", i, err)
		}
	}
}

func TestSearchGateTimeoutIsBusy(t *testing.T) {
	g := testGates()
	g.gateWait = 30 * time.Millisecond
	st := &fakeSearch{result: leninResult(), block: make(chan struct{}), entered: make(chan struct{}, 2)}
	s := newService(Deps{BaseURL: base, Search: st, SearchSlots: limit.New(2)}, g)
	go s.search(context.Background(), SearchInput{Query: "кооперация"})
	<-st.entered
	_, err := s.search(context.Background(), SearchInput{Query: "кооперация"})
	close(st.block)
	if !errors.Is(err, errBusy) || !strings.Contains(userError(err).Error(), "повторите через") {
		t.Fatalf("%v, ожидалось «занято»", err)
	}
}

// Review Focus 4: ушедший агент не держит ворот.
func TestSearchCancelledWhileWaitingLeaksNothing(t *testing.T) {
	shared := limit.New(2)
	st := &fakeSearch{result: leninResult(), block: make(chan struct{}), entered: make(chan struct{}, 2)}
	s := newService(Deps{BaseURL: base, Search: st, SearchSlots: shared}, testGates())
	go s.search(context.Background(), SearchInput{Query: "кооперация"})
	<-st.entered
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := s.search(ctx, SearchInput{Query: "кооперация"})
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("отменённый поиск: %v, ожидалась отмена, а не «занято»", err)
	}
	close(st.block)
	time.Sleep(10 * time.Millisecond)
	if len(s.g.search) != 0 || len(shared) != 0 {
		t.Fatalf("после ухода занято ворот %d, общих %d", len(s.g.search), len(shared))
	}
}

func TestSearchVolume(t *testing.T) {
	st := &fakeSearch{pages: &models.SearchPagesResult{
		Query: "кооперация", Terms: []string{"кооперац"}, Total: 58,
		Pages: []models.SearchPage{{PageNumber: 4, PrintedNumber: 4, ChapterTitle: strp("1. Речь"), Snippet: "о \x01кооперации\x02"}},
	}}
	for i := 0; i < 25; i++ {
		st.pages.Chapters = append(st.pages.Chapters, models.SearchChapterFacet{ID: int64(i + 1), Title: "Гл", Hits: 25 - i})
	}
	works := fakeWorks{114: {ID: 114, Slug: "lenin-t43", Title: "Том 43", Author: "В. И. Ленин"}}
	s := newService(Deps{BaseURL: base, Search: st, SearchSlots: limit.New(2), Works: works}, testGates())
	out, err := s.searchVolume(context.Background(), VolumeSearchInput{Query: "кооперация", Work: 114, Chapters: []int64{7}, Offset: 20})
	if err != nil {
		t.Fatal(err)
	}
	if st.gotWork != 114 || st.gotLimit != volumePageLimit || st.gotOffset != 20 || st.gotChapters[0] != 7 {
		t.Errorf("в хранилище: work %d limit %d offset %d chapters %v", st.gotWork, st.gotLimit, st.gotOffset, st.gotChapters)
	}
	if out.Total != 58 || out.NextOffset != 21 || len(out.Chapters) != maxVolumeChapters {
		t.Errorf("итог: total %d next %d глав %d", out.Total, out.NextOffset, len(out.Chapters))
	}
	if out.Results[0].ID != "/works/114-lenin-t43/pages/4" || out.Results[0].Snippet != "о **кооперации**" {
		t.Errorf("полоса: %+v", out.Results[0])
	}
}

func TestSearchVolumeUnknownWorkIsNotFound(t *testing.T) {
	s := newService(Deps{BaseURL: base, Search: &fakeSearch{}, SearchSlots: limit.New(2), Works: fakeWorks{}}, testGates())
	_, err := s.searchVolume(context.Background(), VolumeSearchInput{Query: "кооперация", Work: 999})
	if !errors.Is(err, ErrNotFound) || userError(err).Error() != msgNotFound {
		t.Fatalf("%v", err)
	}
}
