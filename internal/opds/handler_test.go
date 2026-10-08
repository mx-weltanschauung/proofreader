package opds

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"proofreader/internal/site"
	"strings"
	"testing"
)

type pLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr"`
}

type pEntry struct {
	ID      string  `xml:"id"`
	Title   string  `xml:"title"`
	Summary string  `xml:"summary"`
	Links   []pLink `xml:"link"`
}

type pFeed struct {
	Title   string   `xml:"title"`
	Links   []pLink  `xml:"link"`
	Entries []pEntry `xml:"entry"`
}

func parseFeed(t *testing.T, body string) pFeed {
	t.Helper()
	var f pFeed
	if err := xml.Unmarshal([]byte(body), &f); err != nil {
		t.Fatalf("лента не разбирается: %v\n%s", err, body)
	}
	return f
}

func get(t *testing.T, src Source, target string) (int, http.Header, pFeed, string) {
	t.Helper()
	rec := serve(t, NewHandler(src, testBase+"/"), target)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		return rec.Code, rec.Header(), pFeed{}, body
	}
	return rec.Code, rec.Header(), parseFeed(t, body), body
}

func mustGet(t *testing.T, src Source, target string) pFeed {
	t.Helper()
	code, _, f, body := get(t, src, target)
	if code != http.StatusOK {
		t.Fatalf("%s: %d %s", target, code, body)
	}
	return f
}

func linkOf(ls []pLink, rel, typ string) (pLink, bool) {
	for _, l := range ls {
		if l.Rel == rel && (typ == "" || l.Type == typ) {
			return l, true
		}
	}
	return pLink{}, false
}

func isBook(e pEntry) bool { _, ok := linkOf(e.Links, relAcquisition, ""); return ok }

func titles(es []pEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Title)
	}
	return out
}

func entryByTitle(t *testing.T, f pFeed, title string) pEntry {
	t.Helper()
	for _, e := range f.Entries {
		if e.Title == title {
			return e
		}
	}
	t.Fatalf("нет записи %q среди %q", title, titles(f.Entries))
	return pEntry{}
}

// Каталог подписан именем экземпляра: и заголовок корня, и автор ленты, и
// короткое имя в описании поиска.
func TestCatalogUsesSiteName(t *testing.T) {
	site.Set("Тестовая", "")
	t.Cleanup(func() { site.Set("", "") })
	src := fixtureSource()
	for _, target := range []string{"/opds", "/opds/search.xml"} {
		_, _, _, body := get(t, src, target)
		if !strings.Contains(body, "Тестовая") || strings.Contains(body, "Читальня") {
			t.Errorf("%s подписан не именем экземпляра:\n%s", target, body)
		}
	}
}

func TestRootListsSectionsAndLooseOnlyWhenPresent(t *testing.T) {
	src := fixtureSource()
	f := mustGet(t, src, "/opds")
	want := []string{"Собрания сочинений", "Отдельные тома", "Новые поступления"}
	if strings.Join(titles(f.Entries), "|") != strings.Join(want, "|") {
		t.Fatalf("корень: %q, ждали %q", titles(f.Entries), want)
	}
	if l, ok := linkOf(f.Links, "search", typeOpenSearch); !ok || l.Href != testBase+"/opds/search.xml" {
		t.Errorf("нет ссылки на описание поиска: %+v", f.Links)
	}

	src.shelf.Loose = nil
	f = mustGet(t, src, "/opds")
	for _, e := range f.Entries {
		if e.Title == "Отдельные тома" {
			t.Error("пустые «Отдельные тома» не должны показываться в корне")
		}
	}
}

func TestEditionsSkipEmptyEdition(t *testing.T) {
	f := mustGet(t, fixtureSource(), "/opds/editions")
	if got := titles(f.Entries); len(got) != 1 || got[0] != "И. В. Сталин. Сочинения в 13 томах" {
		t.Fatalf("собрания: %q", got)
	}
	l, ok := linkOf(f.Entries[0].Links, relSubsection, typeNavigation)
	if !ok || l.Href != testBase+"/opds/editions/7" {
		t.Errorf("ссылка в собрание: %+v", f.Entries[0].Links)
	}
}

// Том в ленте собрания — навигация без файлов: у KOReader тап по записи с
// файлами открывает окно скачивания, и до работ внутри не дойти.
func TestEditionVolumeIsNavigationNotBook(t *testing.T) {
	f := mustGet(t, fixtureSource(), "/opds/editions/7")
	if len(f.Entries) != 1 {
		t.Fatalf("тома собрания: %q", titles(f.Entries))
	}
	e := f.Entries[0]
	if isBook(e) {
		t.Error("том в ленте собрания не должен нести файлов")
	}
	if l, ok := linkOf(e.Links, relSubsection, typeAcquisition); !ok || l.Href != testBase+"/opds/works/252" {
		t.Errorf("вход в том: %+v", e.Links)
	}
	if e.Summary != "Сочинения в 13 томах · Том 1 · с. 1—420" {
		t.Errorf("подпись тома: %q", e.Summary)
	}
}

func TestUnknownEditionIs404(t *testing.T) {
	if code, _, _, _ := get(t, fixtureSource(), "/opds/editions/999"); code != http.StatusNotFound {
		t.Fatalf("неизвестное собрание: %d", code)
	}
}

// Правило каталога на неудобном томе: группа годов — папка, лист — книга,
// аппарат и глава без полос не видны, узел из одного аппарата — книга.
func TestWorkFeedFollowsTreeRule(t *testing.T) {
	f := mustGet(t, fixtureSource(), "/opds/works/252")
	want := []string{
		"И. В. Сталин. Сочинения. Том 1 — целиком",
		"1901–1907",
		"Манифест",
		"Речь с комментарием",
	}
	if strings.Join(titles(f.Entries), "|") != strings.Join(want, "|") {
		t.Fatalf("лента тома: %q\nждали %q", titles(f.Entries), want)
	}

	whole := f.Entries[0]
	if l, ok := linkOf(whole.Links, relAcquisition, typeEPUB); !ok || l.Href != testBase+"/api/works/252/download?format=epub" {
		t.Errorf("«целиком» тома: %+v", whole.Links)
	}
	if _, ok := linkOf(whole.Links, relAcquisition, typeFB2); !ok {
		t.Error("у «целиком» нет FB2")
	}

	years := entryByTitle(t, f, "1901–1907")
	if isBook(years) {
		t.Error("группа годов не должна быть книгой")
	}
	if l, ok := linkOf(years.Links, relSubsection, typeAcquisition); !ok || l.Href != testBase+"/opds/works/252/chapters/10" {
		t.Errorf("вход в группу: %+v", years.Links)
	}
	if years.Summary != "Глав: 2 · с. 3—372" {
		t.Errorf("подпись группы: %q", years.Summary)
	}

	speech := entryByTitle(t, f, "Речь с комментарием")
	if l, ok := linkOf(speech.Links, relAcquisition, typeFB2); !ok || l.Href != testBase+"/api/works/252/chapters/30/download?format=fb2" {
		t.Errorf("узел из одного аппарата должен быть книгой: %+v", speech.Links)
	}
	if l, ok := linkOf(speech.Links, relImage, typePNG); !ok || l.Href != testBase+"/og/work/252.png" {
		t.Errorf("обложка главы — карточка тома: %+v", speech.Links)
	}
	if l, ok := linkOf(speech.Links, "alternate", typeHTML); !ok || l.Href != testBase+"/works/252-stalin-t01/chapters/30" {
		t.Errorf("страница в читальне: %+v", speech.Links)
	}
	if up, ok := linkOf(f.Links, "up", typeNavigation); !ok || up.Href != testBase+"/opds/editions/7" {
		t.Errorf("up тома — его собрание: %+v", f.Links)
	}
}

func TestChapterFeedHasWholeThenChildren(t *testing.T) {
	f := mustGet(t, fixtureSource(), "/opds/works/252/chapters/10")
	want := []string{
		"1901–1907 — целиком",
		"Как понимает социал-демократия национальный вопрос?",
		"Письмо из Кутаиса",
	}
	if strings.Join(titles(f.Entries), "|") != strings.Join(want, "|") {
		t.Fatalf("лента группы: %q", titles(f.Entries))
	}
	// up ведёт в ленту тома — ленту книг, и тип ссылки обязан это говорить.
	if up, ok := linkOf(f.Links, "up", typeAcquisition); !ok || up.Href != testBase+"/opds/works/252" {
		t.Errorf("up группы — лента тома: %+v", f.Links)
	}
	whole := f.Entries[0]
	if l, ok := linkOf(whole.Links, relAcquisition, typeEPUB); !ok || l.Href != testBase+"/api/works/252/chapters/10/download?format=epub" {
		t.Errorf("«целиком» группы: %+v", whole.Links)
	}
	if !isBook(f.Entries[1]) || f.Entries[1].Summary != "Сочинения в 13 томах · Том 1 · с. 32—55" {
		t.Errorf("лист: %+v", f.Entries[1])
	}
}

func TestHiddenChaptersAre404(t *testing.T) {
	for _, target := range []string{
		"/opds/works/252/chapters/40",  // аппарат
		"/opds/works/252/chapters/41",  // внутри аппарата
		"/opds/works/252/chapters/31",  // аппарат внутри работы
		"/opds/works/252/chapters/50",  // без полос
		"/opds/works/252/chapters/999", // нет такой
		"/opds/works/999",              // нет тома
	} {
		if code, _, _, _ := get(t, fixtureSource(), target); code != http.StatusNotFound {
			t.Errorf("%s: %d, ждали 404", target, code)
		}
	}
}

func TestWideNodeIsPaged(t *testing.T) {
	src := fixtureSource()
	const path = "/opds/works/253/chapters/60"

	f := mustGet(t, src, path)
	if len(f.Entries) != 1+nodePageSize || f.Entries[0].Title != "Письма — целиком" {
		t.Fatalf("первая страница: %d записей, первая %q", len(f.Entries), f.Entries[0].Title)
	}
	if l, ok := linkOf(f.Links, "next", typeAcquisition); !ok || l.Href != testBase+path+"?page=2" {
		t.Errorf("next первой страницы: %+v", f.Links)
	}
	if _, ok := linkOf(f.Links, "previous", ""); ok {
		t.Error("у первой страницы нет previous")
	}

	f = mustGet(t, src, path+"?page=3")
	if len(f.Entries) != 50 || f.Entries[0].Title != "Письмо 201" {
		t.Fatalf("третья страница: %d записей, первая %q", len(f.Entries), f.Entries[0].Title)
	}
	if _, ok := linkOf(f.Links, "next", ""); ok {
		t.Error("у последней страницы нет next")
	}
	if l, ok := linkOf(f.Links, "previous", ""); !ok || l.Href != testBase+path+"?page=2" {
		t.Errorf("previous: %+v", f.Links)
	}
	if l, ok := linkOf(f.Links, "first", ""); !ok || l.Href != testBase+path {
		t.Errorf("first: %+v", f.Links)
	}
	if l, ok := linkOf(f.Links, "self", ""); !ok || l.Href != testBase+path+"?page=3" {
		t.Errorf("self: %+v", f.Links)
	}

	for _, q := range []string{"?page=4", "?page=0", "?page=-1", "?page=x"} {
		if code, _, _, _ := get(t, src, path+q); code != http.StatusNotFound {
			t.Errorf("%s: %d, ждали 404", q, code)
		}
	}
	// Ровно страница: next нет.
	src.trees[254] = wideTree(254, nodePageSize)
	f = mustGet(t, src, "/opds/works/254/chapters/60")
	if _, ok := linkOf(f.Links, "next", ""); ok {
		t.Error("ровно сто детей — второй страницы нет")
	}
	if code, _, _, _ := get(t, src, "/opds/works/254/chapters/60?page=2"); code != http.StatusNotFound {
		t.Errorf("ровно сто детей, ?page=2: %d, ждали 404", code)
	}
}

func TestNewArrivalsArePagedBooksWithSubsection(t *testing.T) {
	src := fixtureSource()
	src.recent = nil
	for i := 0; i < recentPageSize+5; i++ {
		v := stalinTree().Volume
		v.ID = int64(400 + i)
		src.recent = append(src.recent, v)
	}
	f := mustGet(t, src, "/opds/new")
	if len(f.Entries) != recentPageSize {
		t.Fatalf("первая страница: %d", len(f.Entries))
	}
	e := f.Entries[0]
	if !isBook(e) {
		t.Error("том в новых поступлениях — книга")
	}
	if l, ok := linkOf(e.Links, relSubsection, typeAcquisition); !ok || l.Href != testBase+"/opds/works/400" {
		t.Errorf("вход в дерево тома: %+v", e.Links)
	}
	if _, ok := linkOf(f.Links, "next", ""); !ok {
		t.Error("нет next")
	}
	f = mustGet(t, src, "/opds/new?page=2")
	if len(f.Entries) != 5 {
		t.Fatalf("вторая страница: %d", len(f.Entries))
	}
	if _, ok := linkOf(f.Links, "next", ""); ok {
		t.Error("у последней страницы нет next")
	}
	if code, _, _, _ := get(t, src, "/opds/new?page=3"); code != http.StatusNotFound {
		t.Errorf("пустая третья страница: %d", code)
	}
}

func TestSearchFollowsTreeRule(t *testing.T) {
	src := fixtureSource()
	f := mustGet(t, src, "/opds/search?q=%20%D0%BF%D0%B8%D1%81%D1%8C%D0%BC%D0%BE%20")
	if src.gotQuery != "письмо" {
		t.Errorf("запрос до источника: %q", src.gotQuery)
	}
	want := []string{"Письмо из Кутаиса", "Манифест", "И. В. Сталин. Сочинения. Том 1"}
	if strings.Join(titles(f.Entries), "|") != strings.Join(want, "|") {
		t.Fatalf("выдача: %q\nждали %q", titles(f.Entries), want)
	}
	if !isBook(f.Entries[0]) || isBook(f.Entries[1]) || !isBook(f.Entries[2]) {
		t.Error("лист — книга, узел — навигация, том — книга")
	}
	if got := f.Entries[2].Summary; got != "Сочинения в 13 томах · Том 1 · с. 1—420 · в тексте — 21 полоса" {
		t.Errorf("подпись тома в поиске: %q", got)
	}
}

func TestSearchPagesVolumesAndKeepsQuery(t *testing.T) {
	src := fixtureSource()
	tree := stalinTree()
	src.search.Volumes = nil
	for i := 0; i < searchPageSize+3; i++ {
		v := tree.Volume
		v.ID = int64(500 + i)
		src.search.Volumes = append(src.search.Volumes, VolumeHit{Volume: v, TextHits: 1})
	}
	const target = "/opds/search?q=%D0%BF%D0%B8%D1%81%D1%8C%D0%BC%D0%BE"
	f := mustGet(t, src, target)
	// Две главы (третья в аппарате выпала) и первые тридцать томов.
	if len(f.Entries) != 2+searchPageSize {
		t.Fatalf("первая страница: %d записей", len(f.Entries))
	}
	next, ok := linkOf(f.Links, "next", typeAcquisition)
	if !ok || next.Href != testBase+target+"&page=2" {
		t.Fatalf("next поиска обязан нести запрос: %+v", f.Links)
	}
	f = mustGet(t, src, target+"&page=2")
	// Три оставшихся тома и ни одной главы: главы — только на первой.
	if len(f.Entries) != 3 {
		t.Fatalf("вторая страница: %q", titles(f.Entries))
	}
	if first, ok := linkOf(f.Links, "first", ""); !ok || first.Href != testBase+target {
		t.Errorf("first: %+v", f.Links)
	}
	if code, _, _, _ := get(t, src, target+"&page=3"); code != http.StatusNotFound {
		t.Errorf("пустая третья страница: %d", code)
	}
	// Главы нужны только первой странице — источник не строит их дальше.
	if want := []bool{true, false, false}; fmt.Sprint(src.gotChaps) != fmt.Sprint(want) {
		t.Errorf("главы запрошены по страницам: %v, ждали %v", src.gotChaps, want)
	}
}

// Запрос с «&», «+» и пробелом обязан доехать до второй страницы целым:
// next собирается из q, и неэкранированный «&» отрезал бы хвост запроса.
func TestSearchPagerRoundTripsQuery(t *testing.T) {
	src := fixtureSource()
	v := stalinTree().Volume
	for i := 0; i < searchPageSize+1; i++ {
		src.search.Volumes = append(src.search.Volumes, VolumeHit{Volume: v, TextHits: 1})
	}
	const q = "труд & капитал + рента"
	f := mustGet(t, src, "/opds/search?q="+url.QueryEscape(q))
	next, ok := linkOf(f.Links, "next", "")
	if !ok {
		t.Fatal("нет next")
	}
	u, err := url.Parse(next.Href)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("q") != q || u.Query().Get("page") != "2" {
		t.Fatalf("next %q разбирается в q=%q page=%q", next.Href, u.Query().Get("q"), u.Query().Get("page"))
	}
	mustGet(t, src, u.RequestURI())
	if src.gotQuery != q {
		t.Errorf("вторая страница искала %q", src.gotQuery)
	}
}

func TestSearchRefusalKeepsStatus(t *testing.T) {
	src := fixtureSource()
	src.searchErr = &RequestError{Status: http.StatusServiceUnavailable, Message: "Поиск сейчас занят", RetryAfter: 5}
	code, hdr, _, body := get(t, src, "/opds/search?q=xx")
	if code != http.StatusServiceUnavailable || hdr.Get("Retry-After") != "5" || !strings.Contains(body, "занят") {
		t.Fatalf("отказ поиска: %d %q %q", code, hdr.Get("Retry-After"), body)
	}
	src.searchErr = &RequestError{Status: http.StatusBadRequest, Message: "Слишком короткий запрос"}
	if code, hdr, _, _ := get(t, src, "/opds/search?q=x"); code != http.StatusBadRequest || hdr.Get("Retry-After") != "" {
		t.Fatalf("короткий запрос: %d", code)
	}
}

func TestSourceFailureIs500WithoutPartialFeed(t *testing.T) {
	src := fixtureSource()
	src.err = errors.New("база упала")
	for _, target := range []string{"/opds", "/opds/new", "/opds/works/252"} {
		code, hdr, _, body := get(t, src, target)
		if code != http.StatusInternalServerError || strings.Contains(body, "<feed") ||
			strings.HasPrefix(hdr.Get("Content-Type"), "application/atom") {
			t.Errorf("%s: %d %q %q", target, code, hdr.Get("Content-Type"), body)
		}
	}
}

func TestResponseHeaders(t *testing.T) {
	cases := map[string]string{
		"/opds":                       typeNavigation,
		"/opds/editions/7":            typeNavigation,
		"/opds/works/252":             typeAcquisition,
		"/opds/works/252/chapters/10": typeAcquisition,
		"/opds/new":                   typeAcquisition,
		"/opds/search?q=xx":           typeAcquisition,
		"/opds/search.xml":            typeOpenSearch,
	}
	for target, ctype := range cases {
		rec := serve(t, NewHandler(fixtureSource(), testBase), target)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d", target, rec.Code)
			continue
		}
		h := rec.Header()
		// charset обязателен в заголовке: клиент, не читающий кодировку из
		// XML-объявления, иначе берёт кодировку системы — у читателя с
		// windows-1251 «Читальня» приходила как «Р§РёС‚Р°Р»СЊРЅСЏ».
		if h.Get("Content-Type") != ctype+";charset=utf-8" || h.Get("X-Robots-Tag") != "noindex" ||
			h.Get("Cache-Control") != "public, max-age=300" {
			t.Errorf("%s: %q %q %q", target, h.Get("Content-Type"), h.Get("X-Robots-Tag"), h.Get("Cache-Control"))
		}
	}
}

func TestOpenSearchTemplate(t *testing.T) {
	rec := serve(t, NewHandler(fixtureSource(), testBase), "/opds/search.xml")
	var d struct {
		URL struct {
			Type     string `xml:"type,attr"`
			Template string `xml:"template,attr"`
		} `xml:"Url"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.URL.Template != testBase+"/opds/search?q={searchTerms}" || d.URL.Type != typeAcquisition {
		t.Fatalf("описание поиска: %+v", d.URL)
	}
}

func TestIDsAreStableTags(t *testing.T) {
	f := mustGet(t, fixtureSource(), "/opds/works/252")
	if f.Entries[0].ID != "tag:lib.example.org,2026:work:252" {
		t.Errorf("id тома: %q", f.Entries[0].ID)
	}
	if e := entryByTitle(t, f, "Манифест"); e.ID != "tag:lib.example.org,2026:chapter:20" {
		t.Errorf("id главы: %q", e.ID)
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{1: "полоса", 2: "полосы", 4: "полосы", 5: "полос",
		11: "полос", 12: "полос", 21: "полоса", 22: "полосы", 111: "полос", 0: "полос"} {
		if got := plural(n, "полоса", "полосы", "полос"); got != want {
			t.Errorf("plural(%d) = %q, ждали %q", n, got, want)
		}
	}
}
