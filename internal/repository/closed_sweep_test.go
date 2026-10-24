package repository_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"proofreader/internal/api"
	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/metrics"
	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/internal/stats"
	"proofreader/pkg/storage"
)

// closedMarker стоит во всяком поле закрытого, которое могло бы уехать
// наружу: заглавие и текст полосы, глава, статья указателя, заглавие пункта
// подборки, подпись вклейки, название дорожки, подпись избранного. Найденный
// в ответе анониму или читателю маркер — утечка, где бы он ни нашёлся.
const closedMarker = "ЗАКРЫТО-7f3a"

// chapterMarker — маркер закрытой подглавы открытого тома: её заглавие, текст
// её полос (и общей с соседкой), статья указателя на её полосе, цель события,
// пункт подборки, исходная полоса вклейки. Свой маркер, а не closedMarker:
// сотрудник обязан увидеть именно его, иначе проверка «сотрудник видит»
// прошла бы на закрытом томе рядом.
const chapterMarker = "ЗАКРЫТО-ГЛ-5c1d"

// closedLeak ловит любой из двух маркеров в любом виде, в каком его печатают
// каналы: как есть, строчными, транслитом слага (zakryto-7f3a,
// zakryto-gl-5c1d) и с границами совпадения поиска (U+0001/U+0002) внутри —
// отрывок ts_headline режет маркер по дефису.
var closedLeak = regexp.MustCompile(`(?i)(закрыто|zakryto)[^\p{L}\p{N}]{0,12}(7f3a|(гл|gl)[^\p{L}\p{N}]{0,12}5c1d)`)

// chapterLeak — только маркер закрытой подглавы.
var chapterLeak = regexp.MustCompile(`(?i)(закрыто|zakryto)[^\p{L}\p{N}]{0,12}(гл|gl)[^\p{L}\p{N}]{0,12}5c1d`)

// closedSweep — фикстура сторожа: закрытое всеми четырьмя путями и открытое
// рядом, которое на закрытое ссылается.
type closedSweep struct {
	readerNick, readerTok, editorTok string

	openEd, closedEd int64
	// open — открытый том; self — закрыт сам; front — служебная закрытого
	// тома; edVol — том закрытого издания; issue — номер закрытого журнала.
	open, self, front, edVol, issue int64
	chapters                        map[int64]int64 // работа → глава
	pages                           map[int64]int64 // работа → первая полоса
	journalSlug, person             string
	openJournal, openPerson         string
	closedConcept, openConcept      string
	refs                            []int64
	staffItems, readerItems         []int64
	cuts                            []int64
	track, openTrack, recording     int64
	openRecording                   int64
	suggestion                      int64
	category                        int64
	// События: организация с закрытым событием и смешанным, вторая
	// организация — только закрытое.
	orgA, orgB                          string
	eventClosed, eventMixed, eventOther string
	// Событие вида с маркером в первой организации: вид держит только его,
	// и оно закрыто — вид не должен появиться на странице организации.
	eventKindClosed string

	adminTok string
	// ch — закрытая подглава открытого тома (сторож закрытых глав).
	ch closedChapterSweep
}

// closedChapterSweep — открытый том с закрытой подглавой и всё, что на неё
// или на её полосы ссылается. Том (work) открыт, в собрании openEd, 4 полосы:
//
//	1 — только соседка (открыта);
//	2 — общая: конец соседки и начало закрытой речи (закрыта целиком);
//	3 — только закрытая речь и её подглава (закрыта);
//	4 — заключение родителя (открыта).
//
// Главы: parent «Прения» 1—4 → neighbour 1—2, closed 2—3 (свой признак) →
// grandchild 3—3 (закрыта через предка). Признак ставит администратор
// маршрутом в самом тесте (closeSweepChapter), после того как аноним прогрел
// кэши, — пока его нет, маркер виден всем.
type closedChapterSweep struct {
	work                                  int64
	parent, neighbour, closed, grandchild int64
	pages                                 map[int]int64 // номер полосы → id
	// Звук: дорожка закрытой речи, дорожка соседки через общую полосу,
	// открытая дорожка заключения; записи человека — в закрытой главе и в
	// родителе (его диапазон задевает закрытые полосы).
	track, sharedTrack, openTrack int64
	recording, parentRecording    int64
	// Пункты подборок: на закрытую главу, на родителя, на подглаву под
	// закрытой (читательская подборка).
	staffItem, parentItem, readerItem int64
	// Вклейки сотруднического разбора: с закрытой и с общей полосы.
	cuts []int64
	// Понятие, чья единственная статья выросла из закрытой полосы.
	concept string
	// Организация, у которой все цели — закрытые главы, и её событие;
	// смешанное событие первой организации.
	org, event, eventMixed string
	// suggestion — правка читателя на закрытой полосе, openSuggestion — на
	// открытой полосе того же тома (без неё пустые «мои правки» прошли бы).
	suggestion, openSuggestion int64
	// Номер открытого журнала с закрытой статьёй и человек, подписанный
	// только под ней.
	issue, article int64
	person         string
}

func (f *closedSweep) closedWorks() []int64 { return []int64{f.self, f.front, f.edVol, f.issue} }

func seedClosedSweep(t *testing.T, pool *pgxpool.Pool, authService *auth.Service) *closedSweep {
	t.Helper()
	ctx := context.Background()
	f := &closedSweep{readerNick: "chitatel", chapters: map[int64]int64{}, pages: map[int64]int64{}}
	one := func(sql string, args ...any) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return id
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	reader := one(`INSERT INTO users (password_hash, role, nickname) VALUES ('x', 'reader', $1) RETURNING id`, f.readerNick)
	editor := one(`INSERT INTO users (email, password_hash, role) VALUES ('editor@example.org', 'x', 'editor') RETURNING id`)
	var err error
	nick := f.readerNick
	if f.readerTok, err = authService.GenerateToken(&models.User{ID: reader, Role: models.RoleReader, Nickname: &nick}); err != nil {
		t.Fatal(err)
	}
	if f.editorTok, err = authService.GenerateToken(&models.User{ID: editor, Email: "editor@example.org", Role: models.RoleEditor}); err != nil {
		t.Fatal(err)
	}
	admin := one(`INSERT INTO users (email, password_hash, role) VALUES ('admin@example.org', 'x', 'administrator') RETURNING id`)
	if f.adminTok, err = authService.GenerateToken(&models.User{ID: admin, Email: "admin@example.org", Role: models.RoleAdministrator}); err != nil {
		t.Fatal(err)
	}

	f.openEd = one(`INSERT INTO editions (title, slug, url_slug) VALUES ('Открытое собрание', 'open-ed', 'open') RETURNING id`)
	f.closedEd = one(`INSERT INTO editions (title, slug, url_slug, closed) VALUES ($1, 'closed-ed', 'zakryto-7f3a', true) RETURNING id`,
		"Собрание "+closedMarker)

	work := func(title string, edition *int64, volume *int, role string, parent *int64, closed bool) int64 {
		t.Helper()
		id := one(`INSERT INTO works (title, author, language, country, file_path, owner_id, edition_id,
				volume_number, role, parent_work_id, closed)
			VALUES ($1, 'Автор', 'ru', 'ru', 'pending', 1, $2, $3, $4, $5, $6) RETURNING id`,
			title, edition, volume, role, parent, closed)
		exec(`UPDATE works SET file_path = 'works/' || id || '/original/1_scan.pdf' WHERE id = $1`, id)
		return id
	}
	f.open = work("Открытый том", &f.openEd, ptr(1), models.WorkRoleVolume, nil, false)
	f.self = work("Том "+closedMarker, &f.openEd, ptr(2), models.WorkRoleVolume, nil, true)
	f.front = work("Титул "+closedMarker, nil, nil, models.WorkRoleFrontMatter, &f.self, false)
	f.edVol = work("Том собрания "+closedMarker, &f.closedEd, ptr(1), models.WorkRoleVolume, nil, false)

	journal := one(`INSERT INTO journals (slug, title, closed) VALUES ('zhurnal', $1, true) RETURNING id`, "Журнал "+closedMarker)
	f.journalSlug = "zhurnal"
	f.issue = work(models.IssueTitle("Журнал "+closedMarker, 1928, "5"), nil, nil, models.WorkRoleJournalIssue, nil, false)
	exec(`INSERT INTO journal_issues (journal_id, year, number_from, number_to, label, work_id)
		VALUES ($1, 1928, 5, 5, '5', $2)`, journal, f.issue)

	for _, w := range []int64{f.open, f.self, f.front, f.edVol, f.issue} {
		text := "Открытая полоса о сокровенном"
		title := "Открытая глава"
		if w != f.open {
			text = "Полоса " + closedMarker + " о сокровенном"
			title = "Глава " + closedMarker
		}
		for n := 1; n <= 2; n++ {
			id := one(`INSERT INTO pages (work_id, page_number, content_markdown, preview_path)
				VALUES ($1::bigint, $2::int, $3, 'works/' || $1::bigint || '/pages/page_' || $2::int || '.png') RETURNING id`,
				w, n, fmt.Sprintf("%s, номер %d.", text, n))
			if n == 1 {
				f.pages[w] = id
			}
		}
		var kind *string
		if w == f.issue {
			kind = ptr("статья")
		}
		f.chapters[w] = one(`INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page, article_kind)
			VALUES ($1, $2, 'chapter', 1, 1, 2, $3) RETURNING id`, w, title, kind)
	}

	// Открытый том с подглавой, которую тест закроет (closedChapterSweep).
	// Слово «сокровенном» стоит на открытой и на закрытых полосах: поиск по
	// нему находит том, и отрывок закрытой полосы был бы утечкой.
	f.ch.work = work("Том с прениями", &f.openEd, ptr(3), models.WorkRoleVolume, nil, false)
	f.ch.pages = map[int]int64{}
	for n, text := range map[int]string{
		1: "Открытая речь соседки о сокровенном, полоса 1.",
		2: "Конец речи соседки о сокровенном. Речь " + chapterMarker + " начинается.",
		3: "Речь " + chapterMarker + " о сокровенном продолжается, полоса 3.",
		4: "Открытое заключение прений, полоса 4.",
	} {
		f.ch.pages[n] = one(`INSERT INTO pages (work_id, page_number, content_markdown, preview_path)
			VALUES ($1::bigint, $2::int, $3, 'works/' || $1::bigint || '/pages/page_' || $2::int || '.png') RETURNING id`,
			f.ch.work, n, text)
	}
	chapter := func(title string, parent *int64, order, start, end int) int64 {
		return one(`INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page, parent_id)
			VALUES ($1, $2, 'chapter', $3, $4, $5, $6) RETURNING id`, f.ch.work, title, order, start, end, parent)
	}
	f.ch.parent = chapter("Прения по докладу", nil, 1, 1, 4)
	f.ch.neighbour = chapter("Речь соседки", &f.ch.parent, 1, 1, 2)
	f.ch.closed = chapter("Речь "+chapterMarker, &f.ch.parent, 2, 2, 3)
	f.ch.grandchild = chapter("Подглава речи "+chapterMarker, &f.ch.closed, 1, 3, 3)

	// Подписанная статья номера: человек, у которого все статьи закрыты.
	person := one(`INSERT INTO persons (name, sort_key, slug) VALUES ('Иванов И. И.', 'иванов', 'ivanov') RETURNING id`)
	f.person = "ivanov"
	exec(`INSERT INTO article_credits (chapter_id, position, role, printed, person_id)
		VALUES ($1, 1, 'author', 'И. Иванов', $2)`, f.chapters[f.issue], person)

	// Открытый журнал рядом: его номер, статья и подписанный под ней человек
	// анониму отдаются — иначе маршруты журнала и человека в обходе ничего
	// бы не отдали вовсе.
	f.openJournal, f.openPerson = "otkrytyi", "petrov"
	openJournal := one(`INSERT INTO journals (slug, title) VALUES ($1, 'Открытый журнал') RETURNING id`, f.openJournal)
	openIssue := work(models.IssueTitle("Открытый журнал", 1928, "1"), nil, nil, models.WorkRoleJournalIssue, nil, false)
	exec(`INSERT INTO journal_issues (journal_id, year, number_from, number_to, label, work_id)
		VALUES ($1, 1928, 1, 1, '1', $2)`, openJournal, openIssue)
	exec(`INSERT INTO pages (work_id, page_number, content_markdown, preview_path)
		VALUES ($1::bigint, 1, 'Открытая статья.', 'works/' || $1::bigint || '/pages/page_1.png')`, openIssue)
	openArticle := one(`INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page, article_kind)
		VALUES ($1, 'Открытая статья', 'chapter', 1, 1, 1, 'статья') RETURNING id`, openIssue)
	openPerson := one(`INSERT INTO persons (name, sort_key, slug) VALUES ('Петров П. П.', 'петров', $1) RETURNING id`, f.openPerson)
	exec(`INSERT INTO article_credits (chapter_id, position, role, printed, person_id)
		VALUES ($1, 1, 'author', 'П. Петров', $2)`, openArticle, openPerson)
	// Закрытая статья открытого номера (своим признаком) и человек, у которого
	// других статей нет: страница человека — 404, номер — без статьи.
	f.ch.issue, f.ch.person = openIssue, "sidorov"
	exec(`INSERT INTO pages (work_id, page_number, content_markdown, preview_path)
		VALUES ($1::bigint, 2, $2, 'works/' || $1::bigint || '/pages/page_2.png')`,
		openIssue, "Статья "+chapterMarker+" номера.")
	f.ch.article = one(`INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page, article_kind, closed)
		VALUES ($1, $2, 'chapter', 2, 2, 2, 'статья', true) RETURNING id`, openIssue, "Статья "+chapterMarker)
	sidorov := one(`INSERT INTO persons (name, sort_key, slug) VALUES ('Сидоров С. С.', 'сидоров', $1) RETURNING id`, f.ch.person)
	exec(`INSERT INTO article_credits (chapter_id, position, role, printed, person_id)
		VALUES ($1, 1, 'author', 'С. Сидоров', $2)`, f.ch.article, sidorov)

	f.category = one(`INSERT INTO categories (name, slug, description) VALUES ('Раздел', 'razdel', '') RETURNING id`)
	exec(`INSERT INTO work_categories (work_id, category_id) VALUES ($1, $2), ($3, $2)`, f.self, f.category, f.open)

	// Указатель: понятие одного закрытого собрания (статья с маркером) и
	// понятие открытого собрания с адресами и в открытый, и в закрытый том.
	concept := func(title, slug string) int64 {
		return one(`INSERT INTO index_concepts (title, slug, sort_key, title_key) VALUES ($1, $2, $2, $2) RETURNING id`, title, slug)
	}
	article := func(concept, edition int64, title, text string) int64 {
		return one(`INSERT INTO index_concept_articles (concept_id, edition_id, title, title_key, article_markdown)
			VALUES ($1, $2, $3, $3, $4) RETURNING id`, concept, edition, title, text)
	}
	ref := func(article int64, volume, page, order int) int64 {
		return one(`INSERT INTO index_references (volume_number, page_start, page_end, order_number, article_id)
			VALUES ($1, $2, $2, $3, $4) RETURNING id`, volume, page, order, article)
	}
	f.closedConcept, f.openConcept = "zakrytoe", "otkrytoe"
	ca := article(concept("Сокровенное", f.closedConcept), f.closedEd, "Сокровенное", "Статья "+closedMarker+" указателя.")
	oa := article(concept("Открытое", f.openConcept), f.openEd, "Открытое", "Статья открытого понятия.")
	f.refs = []int64{ref(ca, 1, 1, 1), ref(oa, 1, 1, 1), ref(oa, 2, 1, 2),
		// Открытое понятие: адрес в закрытую полосу тома с прениями и адрес
		// в открытую его полосу.
		ref(oa, 3, 3, 3), ref(oa, 3, 1, 4)}
	// Понятие, единственная статья которого выросла из закрытой полосы
	// (работа-указатель — том с прениями, полоса 3).
	f.ch.concept = "zakrytoe-gl"
	exec(`INSERT INTO index_concept_articles (concept_id, edition_id, work_id, title, title_key, article_markdown,
			source_page_start, source_page_end)
		VALUES ($1, $2, $3, $4, $4, $5, 3, 3)`,
		concept("Понятие "+chapterMarker, f.ch.concept), f.openEd, f.ch.work,
		"Понятие "+chapterMarker, "Статья "+chapterMarker+" указателя.")
	exec(`INSERT INTO index_fragments (reference_id, order_number, start_page_id, start_offset, end_page_id,
			end_offset, head_quote, tail_quote, start_hash, end_hash)
		SELECT $1, 1, id, 0, id, octet_length(content_markdown), 'Полоса', 'номер 1.', repeat('0', 64), repeat('0', 64)
		FROM pages WHERE id = $2`, f.refs[2], f.pages[f.self])

	// Подборки: сотрудническая и читательская под одним слагом.
	staff := one(`INSERT INTO collections (title, slug, owner_id, published_at) VALUES ('Подборка', 'podborka', $1, now()) RETURNING id`, editor)
	readerColl := one(`INSERT INTO collections (title, slug, owner_id, published_at, author_nickname)
		VALUES ('Подборка читателя', 'podborka', $1, now(), $2) RETURNING id`, reader, f.readerNick)
	item := func(coll int64, kind string, chapter *int64, work int64, title string, order int) int64 {
		return one(`INSERT INTO collection_items (collection_id, kind, chapter_id, work_id, snapshot_title,
				snapshot_author, order_number)
			VALUES ($1, $2, $3, $4, $5, 'Автор', $6) RETURNING id`, coll, kind, chapter, work, title, order)
	}
	ch := func(w int64) *int64 { v := f.chapters[w]; return &v }
	f.staffItems = []int64{
		item(staff, "chapter", ch(f.self), f.self, "Пункт "+closedMarker, 1),
		item(staff, "work", nil, f.edVol, "Пункт тома "+closedMarker, 2),
		item(staff, "chapter", ch(f.open), f.open, "Открытый пункт", 3),
	}
	f.ch.staffItem = item(staff, "chapter", &f.ch.closed, f.ch.work, "Пункт "+chapterMarker, 4)
	f.ch.parentItem = item(staff, "chapter", &f.ch.parent, f.ch.work, "Прения по докладу", 5)
	f.readerItems = []int64{
		item(readerColl, "chapter", ch(f.issue), f.issue, "Статья "+closedMarker, 1),
		item(readerColl, "chapter", ch(f.front), f.front, "Титул "+closedMarker, 2),
		item(readerColl, "chapter", ch(f.open), f.open, "Открытый пункт", 3),
	}
	f.ch.readerItem = item(readerColl, "chapter", &f.ch.grandchild, f.ch.work, "Подглава "+chapterMarker, 4)

	// Одобренные разборы, сотруднический и читательский, под одним слагом;
	// вклейки из закрытого и из открытого.
	doc := func(nick string, owner int64) int64 {
		return one(`INSERT INTO documents (title, markdown_content, owner_id, author_nickname, published_title,
				published_markdown, published_at, was_published, review_status, slug)
			VALUES ('Разбор', '', $1, $2, 'Разбор', '', now(), true, 'одобрено', 'razbor') RETURNING id`, owner, nick)
	}
	cut := func(document, page int64, caption string) int64 {
		return one(`INSERT INTO document_cuts (document_id, work_id, start_page_id, start_offset, end_page_id,
				end_offset, head_quote, tail_quote, start_hash, end_hash, source_title)
			SELECT $1, work_id, id, 0, id, octet_length(content_markdown), 'Полоса', 'номер 1.',
				repeat('0', 64), repeat('0', 64), $3
			FROM pages WHERE id = $2 RETURNING id`, document, page, caption)
	}
	body := func(document int64, cuts ...int64) {
		var b strings.Builder
		b.WriteString("Текст разбора.\n\n")
		for _, c := range cuts {
			fmt.Fprintf(&b, "<cut id=\"%d\">\n\n", c)
		}
		exec(`UPDATE documents SET markdown_content = $2, published_markdown = $2 WHERE id = $1`, document, b.String())
	}
	staffDoc, readerDoc := doc("", editor), doc(f.readerNick, reader)
	f.cuts = []int64{
		cut(staffDoc, f.pages[f.self], "Вклейка "+closedMarker),
		cut(staffDoc, f.pages[f.open], "Открытая вклейка"),
		cut(readerDoc, f.pages[f.issue], "Вклейка номера "+closedMarker),
		cut(readerDoc, f.pages[f.open], "Открытая вклейка"),
	}
	// Вклейки с закрытой и с общей полосы тома с прениями: подпись без
	// маркера у второй — утечку выдал бы только текст полосы.
	f.ch.cuts = []int64{
		cut(staffDoc, f.ch.pages[3], "Вклейка "+chapterMarker),
		cut(staffDoc, f.ch.pages[2], "Вклейка с общей полосы"),
	}
	body(staffDoc, f.cuts[0], f.cuts[1], f.ch.cuts[0], f.ch.cuts[1])
	body(readerDoc, f.cuts[2], f.cuts[3])

	// Избранное открытого собрания указывает на главу закрытого тома.
	exec(`INSERT INTO edition_highlights (edition_id, chapter_id, position, label) VALUES ($1, $2, 1, $3)`,
		f.openEd, f.chapters[f.self], "Избранное "+closedMarker)
	exec(`INSERT INTO edition_highlights (edition_id, chapter_id, position, label) VALUES ($1, $2, 2, $3)`,
		f.openEd, f.ch.closed, "Избранное "+chapterMarker)

	// Звук закрытого тома: синтез и запись человека.
	f.track = one(`INSERT INTO audio_tracks (work_id, title, start_page, end_page, s3_key, duration_ms, bytes, md5,
			recipe_sha256, pages_sha256)
		VALUES ($1::bigint, $2, 1, 2, 'works/' || $1::bigint || '/r/1-2.opus', 1000, 10, 'md5', 'recipe', 'pages') RETURNING id`,
		f.self, "Глава "+closedMarker)
	// Звук открытого рядом: редирект на подписанную ссылку — 302.
	f.openTrack = one(`INSERT INTO audio_tracks (work_id, title, start_page, end_page, s3_key, duration_ms, bytes, md5,
			recipe_sha256, pages_sha256)
		VALUES ($1::bigint, 'Открытая глава', 1, 2, 'works/' || $1::bigint || '/r/1-2.opus', 1000, 10, 'md5',
			'recipe', 'pages') RETURNING id`, f.open)
	f.recording = one(`INSERT INTO audio_recordings (work_id, chapter_id, position, reader, s3_key, content_type,
			bytes, duration_ms)
		VALUES ($1::bigint, $2, 1, 'Чтец', 'works/' || $1::bigint || '/rec/1.ogg', 'audio/ogg', 10, 1000) RETURNING id`,
		f.self, f.chapters[f.self])
	f.openRecording = one(`INSERT INTO audio_recordings (work_id, chapter_id, position, reader, s3_key, content_type,
			bytes, duration_ms)
		VALUES ($1::bigint, $2, 1, 'Чтец', 'works/' || $1::bigint || '/rec/1.ogg', 'audio/ogg', 10, 1000) RETURNING id`,
		f.open, f.chapters[f.open])
	track := func(start, end int, title string) int64 {
		return one(`INSERT INTO audio_tracks (work_id, title, start_page, end_page, s3_key, duration_ms, bytes, md5,
				recipe_sha256, pages_sha256)
			VALUES ($1::bigint, $2, $3::int, $4::int, 'works/' || $1::bigint || '/r/' || $3::int || '-' || $4::int || '.opus',
				1000, 10, 'md5', 'recipe', 'pages') RETURNING id`, f.ch.work, title, start, end)
	}
	f.ch.track = track(2, 3, "Речь "+chapterMarker)
	f.ch.sharedTrack = track(1, 2, "Речь соседки")
	f.ch.openTrack = track(4, 4, "Заключение прений")
	recording := func(chapter int64, pos int) int64 {
		return one(`INSERT INTO audio_recordings (work_id, chapter_id, position, reader, s3_key, content_type,
				bytes, duration_ms)
			VALUES ($1::bigint, $2, $3::int, 'Чтец', 'works/' || $1::bigint || '/rec/' || $2::bigint || '.ogg',
				'audio/ogg', 10, 1000) RETURNING id`, f.ch.work, chapter, pos)
	}
	f.ch.recording = recording(f.ch.closed, 1)
	f.ch.parentRecording = recording(f.ch.parent, 1)

	// События. Повестку пишет настоящий ReplaceAgenda. Событие 1 — целиком из
	// закрытого тома (подпись строки и заглавие пункта несут маркер); событие
	// 2 — открытый пункт и строка с маркером, ведущая в закрытый том;
	// у события 2 есть и пункт с маркером, единственная строка которого ведёт
	// в закрытый том, и пустой пункт с маркером — оба аноним не должен
	// увидеть даже заглавием; вторая организация держит только событие
	// 1-го вида, тоже закрытое; у первой организации есть вид с маркером,
	// чьё единственное событие закрыто.
	// Маркер — тот же closedMarker, что у остальной фикстуры.
	f.orgA, f.orgB = "org-a", "org-b"
	f.eventClosed, f.eventMixed, f.eventOther = "event-closed", "event-mixed", "event-other"
	f.eventKindClosed = "event-kind-closed"
	events := repository.NewEventRepository(pool)
	mustEvent := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	orgA := &models.EventOrganization{Slug: f.orgA, Title: "Первая организация", Position: 1}
	orgB := &models.EventOrganization{Slug: f.orgB, Title: "Вторая организация " + closedMarker, Position: 2}
	mustEvent(events.CreateOrganization(ctx, orgA))
	mustEvent(events.CreateOrganization(ctx, orgB))
	kindA := &models.EventKind{OrganizationID: orgA.ID, Title: "Первый вид", Position: 1}
	kindB := &models.EventKind{OrganizationID: orgB.ID, Title: "Вид второй организации", Position: 1}
	kindAClosed := &models.EventKind{OrganizationID: orgA.ID, Title: "Вид " + closedMarker, Position: 2}
	mustEvent(events.CreateKind(ctx, kindA))
	mustEvent(events.CreateKind(ctx, kindB))
	mustEvent(events.CreateKind(ctx, kindAClosed))
	event1 := &models.Event{KindID: kindA.ID, Slug: f.eventClosed, Title: "Событие " + closedMarker, Dates: "1 января", Position: 1}
	event2 := &models.Event{KindID: kindA.ID, Slug: f.eventMixed, Title: "Смешанное событие", Dates: "2 января", Position: 2}
	event3 := &models.Event{KindID: kindB.ID, Slug: f.eventOther, Title: "Событие второй организации", Dates: "3 января", Position: 1}
	event4 := &models.Event{KindID: kindAClosed.ID, Slug: f.eventKindClosed, Title: "Событие вида " + closedMarker, Dates: "4 января", Position: 1}
	for _, e := range []*models.Event{event1, event2, event3, event4} {
		mustEvent(events.CreateEvent(ctx, e))
	}
	chapterTarget := func(w int64, label string) models.AgendaTargetInput {
		return models.AgendaTargetInput{Kind: models.EventTargetChapter, WorkID: w, ChapterID: ptr(f.chapters[w]), Label: label}
	}
	mustEvent(events.ReplaceAgenda(ctx, event1.ID, models.AgendaInput{Sections: []models.AgendaSectionInput{{
		Title: "Пункт " + closedMarker,
		Entries: []models.AgendaEntryInput{{
			Label:   "Строка " + closedMarker,
			Targets: []models.AgendaTargetInput{chapterTarget(f.self, "Цель "+closedMarker)},
		}},
	}}}))
	mustEvent(events.ReplaceAgenda(ctx, event2.ID, models.AgendaInput{Sections: []models.AgendaSectionInput{{
		Title: "Открытый пункт",
		Entries: []models.AgendaEntryInput{
			{Label: "Открытая строка", Targets: []models.AgendaTargetInput{chapterTarget(f.open, "")}},
			{Label: "Строка " + closedMarker, Targets: []models.AgendaTargetInput{
				{Kind: models.EventTargetWork, WorkID: f.edVol, Label: "Том " + closedMarker}}},
		},
	}, {
		Title: "Закрытый пункт " + closedMarker,
		Entries: []models.AgendaEntryInput{{
			Label:   "Строка закрытого пункта " + closedMarker,
			Targets: []models.AgendaTargetInput{chapterTarget(f.self, "")},
		}},
	}, {
		Title: "Пустой пункт " + closedMarker,
	}}}))
	mustEvent(events.ReplaceAgenda(ctx, event4.ID, models.AgendaInput{Sections: []models.AgendaSectionInput{{
		Title: "Пункт события вида " + closedMarker,
		Entries: []models.AgendaEntryInput{{
			Label:   "Строка " + closedMarker,
			Targets: []models.AgendaTargetInput{chapterTarget(f.self, "")},
		}},
	}}}))
	mustEvent(events.ReplaceAgenda(ctx, event3.ID, models.AgendaInput{Sections: []models.AgendaSectionInput{{
		Title: "Пункт второй организации",
		Entries: []models.AgendaEntryInput{{
			Label:   "Строка " + closedMarker,
			Targets: []models.AgendaTargetInput{chapterTarget(f.issue, "")},
		}},
	}}}))

	// События над закрытой подглавой открытого тома. Третья организация
	// держит только цели в закрытых главах — её нет ни на полке, ни по адресу;
	// смешанное событие первой организации отдаёт строку с открытой целью без
	// закрытой цели рядом, а строку и пункт, у которых цели только закрытые, —
	// не отдаёт вовсе.
	f.ch.org, f.ch.event, f.ch.eventMixed = "org-c", "event-gl", "event-gl-mixed"
	chTarget := func(chapter int64, label string) models.AgendaTargetInput {
		return models.AgendaTargetInput{Kind: models.EventTargetChapter, WorkID: f.ch.work, ChapterID: ptr(chapter), Label: label}
	}
	orgC := &models.EventOrganization{Slug: f.ch.org, Title: "Третья организация " + chapterMarker, Position: 3}
	mustEvent(events.CreateOrganization(ctx, orgC))
	kindC := &models.EventKind{OrganizationID: orgC.ID, Title: "Вид третьей организации", Position: 1}
	mustEvent(events.CreateKind(ctx, kindC))
	eventC := &models.Event{KindID: kindC.ID, Slug: f.ch.event, Title: "Событие " + chapterMarker, Dates: "5 января", Position: 1}
	eventMixedC := &models.Event{KindID: kindA.ID, Slug: f.ch.eventMixed, Title: "Прения открытого тома", Dates: "6 января", Position: 3}
	mustEvent(events.CreateEvent(ctx, eventC))
	mustEvent(events.CreateEvent(ctx, eventMixedC))
	mustEvent(events.ReplaceAgenda(ctx, eventC.ID, models.AgendaInput{Sections: []models.AgendaSectionInput{{
		Title: "Пункт " + chapterMarker,
		Entries: []models.AgendaEntryInput{{
			Label: "Строка " + chapterMarker,
			Targets: []models.AgendaTargetInput{chTarget(f.ch.closed, "Цель "+chapterMarker),
				chTarget(f.ch.grandchild, "")},
		}},
	}}}))
	mustEvent(events.ReplaceAgenda(ctx, eventMixedC.ID, models.AgendaInput{Sections: []models.AgendaSectionInput{{
		Title: "Открытый пункт прений",
		Entries: []models.AgendaEntryInput{
			{Label: "Открытая речь", Targets: []models.AgendaTargetInput{chTarget(f.ch.neighbour, ""),
				chTarget(f.ch.closed, "Цель "+chapterMarker)}},
			{Label: "Строка " + chapterMarker, Targets: []models.AgendaTargetInput{chTarget(f.ch.closed, "")}},
		},
	}, {
		Title: "Пункт " + chapterMarker,
		Entries: []models.AgendaEntryInput{{
			Label:   "Строка подглавы",
			Targets: []models.AgendaTargetInput{chTarget(f.ch.grandchild, "")},
		}},
	}}}))

	// Правка читателя на полосе, которую потом закрыли.
	f.suggestion = one(`INSERT INTO page_suggestions (page_id, base_markdown, proposed_markdown, user_id)
		SELECT id, content_markdown, content_markdown || ' правка', $2 FROM pages WHERE id = $1 RETURNING id`,
		f.pages[f.self], reader)
	// Правка читателя на полосе подглавы, которую потом закроют: в «моих
	// правках» её нет (номер полосы и заглавие тома выдали бы закрытое место).
	f.ch.suggestion = one(`INSERT INTO page_suggestions (page_id, base_markdown, proposed_markdown, user_id)
		SELECT id, content_markdown, content_markdown || ' правка', $2 FROM pages WHERE id = $1 RETURNING id`,
		f.ch.pages[3], reader)
	f.ch.openSuggestion = one(`INSERT INTO page_suggestions (page_id, base_markdown, proposed_markdown, user_id)
		SELECT id, content_markdown, content_markdown || ' правка', $2 FROM pages WHERE id = $1 RETURNING id`,
		f.ch.pages[1], reader)
	return f
}

// sweepStorage — хранилище без сети: подписанная ссылка несёт ключ объекта,
// и ключ закрытой работы в ответе виден как утечка скана.
type sweepStorage struct{}

func (sweepStorage) Put(context.Context, string, io.Reader, int64, string) error { return nil }
func (sweepStorage) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, storage.ErrObjectNotFound
}
func (sweepStorage) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://s3.invalid/" + key, nil
}
func (sweepStorage) PresignGetAttachment(_ context.Context, key string, _ time.Duration, _ string) (string, error) {
	return "https://s3.invalid/" + key, nil
}
func (sweepStorage) PresignPut(_ context.Context, key, _ string, _ time.Duration) (string, error) {
	return "https://s3.invalid/" + key, nil
}
func (sweepStorage) Head(context.Context, string) (int64, string, error) {
	return 0, "", storage.ErrObjectNotFound
}
func (sweepStorage) Delete(context.Context, string) error       { return nil }
func (sweepStorage) DeletePrefix(context.Context, string) error { return nil }

// closedSweepRouter — роутер читальни той же сборкой, что cmd/server
// (api.NewServer), над одноразовой базой; файловый кэш глав включён, чтобы
// прогретая сотрудником глава была в нём по-настоящему.
func closedSweepRouter(t *testing.T, pool *pgxpool.Pool) (*mux.Router, *auth.Service, *pagecache.Store) {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{PublicBaseURL: sweepBase},
		JWT: config.JWTConfig{Secret: "closed-sweep", Expiration: time.Hour,
			RefreshExpiration: time.Hour, ReaderExpiration: time.Hour},
		S3: config.S3Config{PresignTTL: time.Minute},
	}
	cache := pagecache.New(t.TempDir())
	if err := cache.Init("sweep"); err != nil {
		t.Fatal(err)
	}
	r := api.NewServer(api.ServerDeps{
		Config: cfg, Pool: pool, Commit: "sweep", Build: "sweep", PageCache: cache,
		Scans: sweepStorage{}, Audio: sweepStorage{},
		Stats:   stats.NewRecorder(repository.NewStatsRepository(pool)),
		Metrics: metrics.New(),
	})
	return r, auth.NewService(&cfg.JWT), cache
}

// closedSweepVars — значения переменных пути для обхода маршрутов. Ключ —
// ближайший буквальный сегмент перед переменной и сама переменная
// («works/{id}», «items/{itemId}»): одно имя {id} значит разное под разными
// сегментами. Переменная маршрута без значения здесь роняет тест — новый
// маршрут не уйдёт мимо сторожа неназванным.
func closedSweepVars(f *closedSweep) map[string][]string {
	ids := func(v ...int64) []string {
		out := make([]string, len(v))
		for i, id := range v {
			out[i] = itoa(id)
		}
		return out
	}
	works := ids(f.open, f.self, f.front, f.edVol, f.issue, f.ch.work)
	var chapters, pages []string
	for _, w := range []int64{f.open, f.self, f.front, f.edVol, f.issue} {
		chapters = append(chapters, itoa(f.chapters[w]))
		pages = append(pages, itoa(f.pages[w]))
	}
	chapters = append(chapters, ids(f.ch.parent, f.ch.neighbour, f.ch.closed, f.ch.grandchild)...)
	for n := 1; n <= 4; n++ {
		pages = append(pages, itoa(f.ch.pages[n]))
	}
	return map[string][]string{
		"works/{id}":             works,
		"works/{workId}":         works,
		"chapters/{id}":          chapters,
		"pages/{pageId}":         pages,
		"by-number/{pageNumber}": {"1", "3"},
		"versions/{versionId}":   {"1"},
		"editions/{id}":          ids(f.openEd, f.closedEd),
		"journals/{slug}":        {f.journalSlug, f.openJournal},
		"persons/{slug}":         {f.person, f.openPerson, f.ch.person},
		"categories/{id}":        ids(f.category),
		"concepts/{slug}":        {f.closedConcept, f.openConcept, f.ch.concept},
		"references/{refId}":     ids(f.refs...),
		"audio/{id}":             ids(f.track, f.openTrack, f.ch.track, f.ch.sharedTrack, f.ch.openTrack),
		"rec/{id}":               ids(f.recording, f.openRecording, f.ch.recording, f.ch.parentRecording),
		"suggestions/{id}":       ids(f.suggestion, f.ch.suggestion),
		"documents/{slug}":       {"razbor"},
		"documents/{nickname}":   {f.readerNick},
		"cuts/{cutId}":           ids(append(append([]int64{}, f.cuts...), f.ch.cuts...)...),
		"collections/{slug}":     {"podborka"},
		"collections/{nickname}": {f.readerNick},
		"items/{itemId}": ids(append(append(append([]int64{}, f.staffItems...), f.readerItems...),
			f.ch.staffItem, f.ch.parentItem, f.ch.readerItem)...),
		"readers/{nickname}":        {f.readerNick},
		"organizations/{slug}":      {f.orgA, f.orgB, f.ch.org},
		"organizations/{eventSlug}": {f.eventClosed, f.eventMixed, f.eventOther, f.eventKindClosed, f.ch.event, f.ch.eventMixed},
	}
}

// closedSweepQueries — параметры, без которых маршрут отвечает отказом и
// закрытого не показал бы, даже если бы протекал.
func closedSweepQueries(f *closedSweep) map[string][]string {
	q := url.QueryEscape("ЗАКРЫТО")
	var searchPages []string
	for _, w := range append(f.closedWorks(), f.open, f.ch.work) {
		searchPages = append(searchPages, "q="+q+"&work_id="+itoa(w))
	}
	all := strings.Join([]string{itoa(f.open), itoa(f.self), itoa(f.front), itoa(f.edVol), itoa(f.issue), itoa(f.ch.work)}, ",")
	downloads := []string{"format=md", "format=html", "format=fb2", "format=m3u"}
	return map[string][]string{
		"/api/search": {"q=" + q, "q=" + url.QueryEscape("сокровенном"), "q=" + q + "&terms_only=1",
			"q=" + q + "&editions=" + itoa(f.openEd) + "," + itoa(f.closedEd), "q=" + q + "&works=" + all},
		"/api/search/pages":        append(searchPages, "q="+url.QueryEscape("сокровенном")+"&work_id="+itoa(f.ch.work)),
		"/api/works":               {"", "limit=500"},
		"/api/persons":             {"q=" + url.QueryEscape("Иван")},
		"/api/concepts":            {"", "q=" + url.QueryEscape("Сокровен")},
		"/api/works/{id}/download": downloads,
		"/api/works/{workId}/chapters/{id}/download":  downloads,
		"/api/collections/{slug}/download":            downloads,
		"/api/collections/{nickname}/{slug}/download": downloads,
		"/api/works/{id}/audio":                       {"", "format=m3u"},
		"/api/works/{workId}/reading":                 {"", "from=1&count=50"},
		"/api/readers":                                {"q=" + f.readerNick},
	}
}

// closedSweepSilent — маршруты GET, которым позволено не отдать анониму и
// читателю ничего (ни 2xx, ни 3xx), с доводом. Остальные молчащие роняют
// тест: молчание значит, что маршрут в обходе не показал ни одного ответа с
// данными, и утечка в них прошла бы незамеченной.
func closedSweepSilent() map[string]string {
	const admin = "только администратор: анониму 401, читателю 403"
	const staff = "только сотрудник: анониму 401, читателю 403"
	return map[string]string{
		"/api/admin/stats/crawlers":                               admin,
		"/api/admin/stats/devices":                                admin,
		"/api/admin/stats/health":                                 admin,
		"/api/admin/stats/referrers":                              admin,
		"/api/admin/stats/searches":                               admin,
		"/api/admin/stats/top":                                    admin,
		"/api/admin/stats/traffic":                                admin,
		"/api/cache/stats":                                        admin,
		"/api/admin/closures":                                     admin,
		"/api/works/{workId}/chapters/{id}/closed-plan":           admin,
		"/api/export":                                             admin,
		"/api/feedback":                                           admin,
		"/api/feedback/unread-count":                              admin,
		"/api/readers":                                            admin,
		"/api/readers/{nickname}/collections":                     admin,
		"/api/users":                                              admin,
		"/api/audio/queue":                                        staff,
		"/api/documents/review":                                   staff,
		"/api/suggestions":                                        staff,
		"/api/suggestions/{id}":                                   staff,
		"/api/works/{id}/apparatus":                               staff,
		"/api/closures/latest":                                    staff,
		"/api/works/{workId}/pages/{pageId}/versions":             staff,
		"/api/works/{workId}/pages/{pageId}/versions/{versionId}": staff,
		"/api/static-archive":                                     "архива в тесте нет (STATIC_ARCHIVE_URL пуст) — 404 всем",
		"/api/static-archive/download":                            "архива в тесте нет (STATIC_ARCHIVE_URL пуст) — 404 всем",
		"/mcp":                                                    "GET отвечает 405 по устройству; MCP проверяет sweepMCP",
		"/og/":                                                    "голый префикс — 404; карточки обходит sweepTokenless",
		"/sitemap-":                                               "голый префикс — 404; куски карты обходит sweepTokenless от /sitemap.xml",
	}
}

// closedDirectKeys — переменные, значение которых само по себе адрес
// закрытого: такой адрес анониму и читателю отвечает 404 (или отказом в
// доступе раньше, у сотруднического маршрута).
func closedDirectValues(f *closedSweep) map[string]map[string]bool {
	set := func(v ...string) map[string]bool {
		m := map[string]bool{}
		for _, s := range v {
			m[s] = true
		}
		return m
	}
	works := set(itoa(f.self), itoa(f.front), itoa(f.edVol), itoa(f.issue))
	return map[string]map[string]bool{
		"works/{id}":      works,
		"works/{workId}":  works,
		"editions/{id}":   set(itoa(f.closedEd)),
		"journals/{slug}": set(f.journalSlug),
		"concepts/{slug}": set(f.closedConcept, f.ch.concept),
		"persons/{slug}":  set(f.person, f.ch.person),
		"audio/{id}":      set(itoa(f.track), itoa(f.ch.track), itoa(f.ch.sharedTrack)),
		"rec/{id}":        set(itoa(f.recording), itoa(f.ch.recording), itoa(f.ch.parentRecording)),
		// Вторая организация и событие 1 видны только закрытым: 404 всем,
		// кроме сотрудника.
		"organizations/{slug}":      set(f.orgB, f.ch.org),
		"organizations/{eventSlug}": set(f.eventClosed, f.eventKindClosed, f.ch.event),
		// Закрытая подглава и её потомок, закрытые полосы (общая с соседкой —
		// тоже) и номер закрытой полосы: 404 в любом сочетании.
		"chapters/{id}":          set(itoa(f.ch.closed), itoa(f.ch.grandchild)),
		"pages/{pageId}":         set(itoa(f.ch.pages[2]), itoa(f.ch.pages[3])),
		"by-number/{pageNumber}": set("3"),
	}
}

var sweepPathVar = regexp.MustCompile(`\{([A-Za-z]+)(?::[^}]*)?\}`)

// sweepURL — один развёрнутый адрес маршрута и признак «прямой адрес
// закрытого».
type sweepURL struct {
	path   string
	direct bool
}

// expandRoute подставляет в шаблон все сочетания значений его переменных.
func expandRoute(t *testing.T, tpl string, vars map[string][]string, direct map[string]map[string]bool) []sweepURL {
	t.Helper()
	segs := strings.Split(tpl, "/")
	type slot struct {
		seg  int
		key  string
		expr string
	}
	var slots []slot
	lit := ""
	for i, s := range segs {
		ms := sweepPathVar.FindAllStringSubmatch(s, -1)
		if len(ms) == 0 {
			lit = s
			continue
		}
		for _, m := range ms {
			key := lit + "/{" + m[1] + "}"
			if _, ok := vars[key]; !ok {
				t.Fatalf("маршрут %s: заведи значение для %s в closedSweepVars", tpl, key)
			}
			slots = append(slots, slot{i, key, m[0]})
		}
	}
	out := []sweepURL{{path: tpl}}
	for _, sl := range slots {
		var next []sweepURL
		for _, u := range out {
			for _, v := range vars[sl.key] {
				next = append(next, sweepURL{
					path:   strings.Replace(u.path, sl.expr, v, 1),
					direct: u.direct || direct[sl.key][v],
				})
			}
		}
		out = next
	}
	// Самый широкий — понятие × адрес × полоса (3 × 5 × 9).
	if len(out) > 160 {
		t.Fatalf("маршрут %s разворачивается в %d адресов — сократи значения", tpl, len(out))
	}
	return out
}

type sweepResponse struct {
	code int
	text string // тело (разжатое) и заголовки, по которым уходят адреса
	loc  string
}

func sweepDo(t *testing.T, h http.Handler, path, token string) sweepResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	// Как браузер: кэш глав отдаёт готовый gzip, и проверяется разжатое тело.
	req.Header.Set("Accept-Encoding", "gzip")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Errorf("%s: паника обработчика: %v", path, p)
				rec.Code = http.StatusInternalServerError
			}
		}()
		h.ServeHTTP(rec, req)
	}()
	body := rec.Body.Bytes()
	if rec.Header().Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("%s: gzip: %v", path, err)
		}
		if body, err = io.ReadAll(zr); err != nil {
			t.Fatalf("%s: gzip: %v", path, err)
		}
	}
	text := string(body) + "\n" + rec.Header().Get("Location") + "\n" + strings.Join(rec.Header().Values("Link"), "\n")
	return sweepResponse{code: rec.Code, text: text, loc: rec.Header().Get("Location")}
}

// sweepFollow — sweepDo, который идёт по постоянному редиректу краулера на
// канонический адрес (голый номер → номер со слагом).
func sweepFollow(t *testing.T, h http.Handler, path, token string) sweepResponse {
	t.Helper()
	res := sweepDo(t, h, path, token)
	for i := 0; i < 3 && res.code == http.StatusMovedPermanently; i++ {
		loc := strings.TrimPrefix(res.loc, sweepBase)
		if strings.HasPrefix(path, "/seo") && !strings.HasPrefix(loc, "/seo") {
			loc = "/seo" + loc
		}
		res = sweepDo(t, h, loc, token)
	}
	return res
}

// percentRun — подряд идущие %XX: один закодированный знак UTF-8 занимает
// несколько.
var percentRun = regexp.MustCompile(`(?:%[0-9A-Fa-f]{2})+`)

// percentDecoded раскодирует %XX кусками, а не телом целиком: одиночный «%»
// в тексте («50%») ломал бы url.QueryUnescape всего тела, и раскодированная
// проверка молча пропускалась бы. Кусок, который не раскодируется, остаётся
// как есть.
func percentDecoded(text string) string {
	return percentRun.ReplaceAllStringFunc(text, func(run string) string {
		if dec, err := url.PathUnescape(run); err == nil {
			return dec
		}
		return run
	})
}

// leakIn — что именно в ответе выдаёт закрытое: маркер в любом виде или ключ
// скана либо звука закрытой работы.
func leakIn(f *closedSweep, text string) string {
	norm := strings.NewReplacer(`\u0001`, "", `\u0002`, "", "\x01", "", "\x02", "").Replace(text)
	norm += "\n" + percentDecoded(norm)
	if m := closedLeak.FindString(norm); m != "" {
		return fmt.Sprintf("маркер %q", m)
	}
	for _, w := range f.closedWorks() {
		if key := "s3.invalid/works/" + itoa(w) + "/"; strings.Contains(norm, key) {
			return "ключ хранилища " + key
		}
	}
	for _, key := range f.ch.hiddenKeys() {
		if strings.Contains(norm, key) {
			return "ключ хранилища " + key
		}
	}
	return ""
}

// hiddenKeys — объекты тома с закрытой подглавой, которых анониму не
// выдают: оригинал тома (в нём и закрытая речь), сканы закрытых полос,
// дорожки, задевшие закрытую полосу, записи человека в закрытой главе и в
// родителе поверх неё. Пустая фикстура (TestLeakIn…) — пустой список.
func (c closedChapterSweep) hiddenKeys() []string {
	if c.work == 0 {
		return nil
	}
	w := "s3.invalid/works/" + itoa(c.work) + "/"
	return []string{
		w + "original/",
		w + "pages/page_2.png", w + "pages/page_3.png",
		w + "r/2-3.opus", w + "r/1-2.opus",
		w + "rec/" + itoa(c.closed) + ".ogg", w + "rec/" + itoa(c.parent) + ".ogg",
	}
}

// TestClosedNeverReachesOutsiders — сквозной сторож закрытого: закрытое
// любым из четырёх путей (само; служебная закрытого тома; том закрытого
// собрания; номер закрытого журнала) и закрытая подглава открытого тома
// (closedChapterSweep: её полосы, общая с соседкой, всё, что на неё
// ссылается) анониму и читателю не отдаётся ни одним маршрутом GET роутера,
// ни бестокенными каналами (краулер, тексты для нейросетей, карта сайта,
// OPDS, MCP), а сотруднику отдаётся — иначе фикстура ничего бы не доказывала.
//
// Маршруты берутся обходом настоящего роутера, а не списком: маршрут,
// заведённый позже, попадает в обход сам, и если его переменной нет в
// closedSweepVars, тест падает, пока её не заведут.
func TestClosedNeverReachesOutsiders(t *testing.T) {
	pool := repository.SharedTestPool(t)
	router, authService, cache := closedSweepRouter(t, pool)
	f := seedClosedSweep(t, pool, authService)
	// Журнал запросов (middleware.Logging) — тысячи строк на прогон.
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	// Открытое, которое ссылается на закрытое, анониму отдаётся: без этого
	// «маркера нет» значило бы только, что адреса неверны.
	openCh := itoa(f.chapters[f.open])
	for _, path := range []string{
		"/api/works/" + itoa(f.open), "/api/works/" + itoa(f.open) + "/pages",
		"/api/works/" + itoa(f.open) + "/chapters/" + openCh + "/pages",
		"/api/editions/" + itoa(f.openEd) + "/works", "/api/editions/" + itoa(f.openEd) + "/highlights",
		"/api/shelf", "/api/collections/podborka", "/api/collections/" + f.readerNick + "/podborka",
		"/api/documents/razbor/view", "/api/documents/" + f.readerNick + "/razbor/view",
		"/api/concepts/" + f.openConcept, "/api/categories/" + itoa(f.category),
		"/api/organizations/" + f.orgA, "/api/organizations/" + f.orgA + "/" + f.eventMixed,
		"/api/works/" + itoa(f.open) + "/chapters/" + openCh + "/events",
	} {
		if res := sweepDo(t, router, path, ""); res.code != http.StatusOK {
			t.Errorf("аноним: открытое %s отвечает %d — проверка ничего не значит", path, res.code)
		}
	}

	closeSweepChapter(t, router, f, cache)
	checkClosedChapter(t, router, f, cache)

	// Сотрудник видит закрытое — и заодно прогревает кэш главы, который
	// анониму обязан не достаться.
	selfCh := itoa(f.chapters[f.self])
	staffSees := []string{
		"/api/works/" + itoa(f.self),
		"/api/works/" + itoa(f.self) + "/pages",
		"/api/works/" + itoa(f.self) + "/chapters/" + selfCh + "/pages",
		"/api/search?q=" + url.QueryEscape("ЗАКРЫТО"),
		"/api/journals/" + f.journalSlug,
		"/api/works/" + itoa(f.self) + "/download?format=md",
	}
	for _, w := range []int64{f.front, f.edVol, f.issue} {
		staffSees = append(staffSees, "/api/works/"+itoa(w), "/api/works/"+itoa(w)+"/pages",
			"/api/works/"+itoa(w)+"/chapters/"+itoa(f.chapters[w])+"/pages")
	}
	// Прямые адреса, на которые аноним получает 404, сотруднику обязаны
	// отдавать закрытое: неверный id фикстуры иначе дал бы 404 всем, и
	// проверка анонима прошла бы впустую.
	staffSees = append(staffSees,
		"/api/editions/"+itoa(f.closedEd),
		"/api/persons/"+f.person,
		"/api/concepts/"+f.closedConcept,
		"/api/organizations/"+f.orgB,
		"/api/organizations/"+f.orgA+"/"+f.eventClosed,
		"/api/organizations/"+f.orgA+"/"+f.eventKindClosed,
		"/api/organizations/"+f.orgB+"/"+f.eventOther,
	)
	for _, path := range staffSees {
		res := sweepDo(t, router, path, f.editorTok)
		if res.code != http.StatusOK || leakIn(f, res.text) == "" {
			t.Errorf("сотрудник: %s (%d) не видит закрытого — фикстура ничего не доказывает", path, res.code)
		}
	}
	// События: закрытое событие и организация, у которой видимого нет, аноним
	// и читатель получают 404; смешанное событие показывает открытую строку и
	// не показывает строку с маркером.
	for _, tok := range []string{"", f.readerTok} {
		for _, path := range []string{
			"/api/organizations/" + f.orgA + "/" + f.eventClosed,
			"/api/organizations/" + f.orgA + "/" + f.eventKindClosed,
			"/api/organizations/" + f.orgB,
			"/api/organizations/" + f.orgB + "/" + f.eventOther,
		} {
			if res := sweepDo(t, router, path, tok); res.code != http.StatusNotFound {
				t.Errorf("события: %s отвечает %d, ждали 404", path, res.code)
			}
		}
		res := sweepDo(t, router, "/api/organizations/"+f.orgA+"/"+f.eventMixed, tok)
		if res.code != http.StatusOK || !strings.Contains(res.text, "Открытая строка") || leakIn(f, res.text) != "" {
			t.Errorf("события: смешанное событие (%d) должно отдать открытую строку без маркера", res.code)
		}
		// Страница организации: вид с маркером держит только закрытое
		// событие — его нет вовсе, открытое событие на месте.
		res = sweepDo(t, router, "/api/organizations/"+f.orgA, tok)
		if res.code != http.StatusOK || !strings.Contains(res.text, f.eventMixed) || leakIn(f, res.text) != "" {
			t.Errorf("события: организация (%d) должна отдать смешанное событие без вида с маркером", res.code)
		}
	}
	// Звук закрытого сотруднику — редирект на подписанную ссылку своего ключа.
	for path, key := range map[string]string{
		"/api/audio/" + itoa(f.track) + ".opus": "s3.invalid/works/" + itoa(f.self) + "/r/",
		"/api/audio/rec/" + itoa(f.recording):   "s3.invalid/works/" + itoa(f.self) + "/rec/",
	} {
		res := sweepDo(t, router, path, f.editorTok)
		if res.code != http.StatusFound || !strings.Contains(res.loc, key) {
			t.Errorf("сотрудник: %s (%d, %q) — ждали 302 на %s", path, res.code, res.loc, key)
		}
	}
	for _, w := range f.closedWorks() {
		rc, _, ok := cache.Open(pagecache.Key{WorkID: w, Start: 1, End: 2})
		if !ok {
			t.Fatalf("глава закрытой работы %d не легла в кэш — проверка кэша ничего не значит", w)
		}
		rc.Close()
	}

	vars := closedSweepVars(f)
	queries := closedSweepQueries(f)
	direct := closedDirectValues(f)
	var routes, prefixes []string
	if err := router.Walk(func(r *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		tpl, err := r.GetPathTemplate()
		if err != nil {
			return nil
		}
		methods, err := r.GetMethods()
		if err != nil {
			return nil
		}
		for _, m := range methods {
			if m == http.MethodGet {
				routes = append(routes, tpl)
				// PathPrefix: регулярка без «$» — канал под своим префиксом
				// (краулер, карта сайта, карточки, OPDS). Его голый адрес
				// идёт в обход по ссылкам.
				if re, err := r.GetPathRegexp(); err == nil && !strings.HasSuffix(re, "$") {
					prefixes = append(prefixes, tpl)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(routes)

	var requests, served int
	var silent []string
	for _, tpl := range routes {
		before := served
		qs := queries[tpl]
		if qs == nil {
			qs = []string{""}
		}
		for _, u := range expandRoute(t, tpl, vars, direct) {
			for _, q := range qs {
				path := u.path
				if q != "" {
					path += "?" + q
				}
				for _, who := range []struct{ name, token string }{{"аноним", ""}, {"читатель", f.readerTok}} {
					requests++
					res := sweepDo(t, router, path, who.token)
					if leak := leakIn(f, res.text); leak != "" {
						t.Errorf("%s: %s (%d) — %s", who.name, path, res.code, leak)
					}
					if res.code < http.StatusBadRequest {
						served++
					}
					if u.direct && res.code != http.StatusNotFound &&
						res.code != http.StatusUnauthorized && res.code != http.StatusForbidden {
						t.Errorf("%s: прямой адрес закрытого %s отвечает %d, ждали 404", who.name, path, res.code)
					}
				}
			}
		}
		if served == before {
			silent = append(silent, tpl)
		}
	}
	// Маршрут, не отдавший ничего, проверен только на отказ: данных он не
	// показал, и утечку в них обход увидеть не мог. Молчать вправе только
	// названные в closedSweepSilent; новому молчащему — запрос в
	// closedSweepQueries или довод в списке.
	allowed := closedSweepSilent()
	silentSet := map[string]bool{}
	for _, tpl := range silent {
		silentSet[tpl] = true
		if _, ok := allowed[tpl]; !ok {
			t.Errorf("маршрут %s ничего не отдал анониму и читателю: заведи ему запрос в closedSweepQueries "+
				"(или данные в фикстуре) либо объясни молчание в closedSweepSilent", tpl)
		}
	}
	for tpl := range allowed {
		if !silentSet[tpl] {
			t.Errorf("маршрут %s в closedSweepSilent, но не молчит или его нет — убери из списка", tpl)
		}
	}
	t.Logf("обход роутера: маршрутов GET %d, запросов %d, из них отдано (2xx/3xx) — %d", len(routes), requests, served)
	t.Logf("маршруты, не отдавшие ничего анониму и читателю (сотруднические, закрытые целиком, без данных): %s",
		strings.Join(silent, " "))
	if len(routes) < 80 || served < 100 {
		t.Errorf("обход подозрительно мал или глух: маршрутов %d, отдано %d из %d", len(routes), served, requests)
	}

	sweepTokenless(t, router, f, prefixes)
	sweepMCP(t, router, f)
}

// closeSweepChapter — аноним прогревает кэши тома с прениями, пока подглава
// открыта (маркер виден — фикстура верна), администратор закрывает её
// маршрутом, а потом устаревший файл главы-родителя кладётся в кэш обратно:
// так выглядел бы файл, переживший сброс (другой процесс, гонка). Проверка
// закрытости обязана идти раньше кэша, а не надеяться на сброс.
func closeSweepChapter(t *testing.T, router http.Handler, f *closedSweep, cache *pagecache.Store) {
	t.Helper()
	c := f.ch
	vol := "/api/works/" + itoa(c.work)
	parentPages := vol + "/chapters/" + itoa(c.parent) + "/pages"
	for _, path := range []string{
		parentPages, vol + "/chapters/" + itoa(c.neighbour) + "/pages",
		"/seo/works/" + itoa(c.work) + "/chapters/" + itoa(c.parent),
		"/seo/works/" + itoa(c.work) + "/chapters/" + itoa(c.parent) + ".md",
		"/seo/works/" + itoa(c.work) + ".md",
	} {
		res := sweepFollow(t, router, path, "")
		if res.code != http.StatusOK || chapterLeak.FindString(res.text) == "" {
			t.Fatalf("до закрытия: %s (%d) не отдаёт подглаву анониму — фикстура неверна", path, res.code)
		}
	}
	parentKey := pagecache.Key{WorkID: c.work, Start: 1, End: 4}
	rc, _, ok := cache.Open(parentKey)
	if !ok {
		t.Fatal("глава-родитель не легла в кэш до закрытия — проверка кэша ничего не значит")
	}
	zr, err := gzip.NewReader(rc)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := io.ReadAll(zr)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Редактор закрыть не может, администратор — да.
	put := func(token string) int {
		req := httptest.NewRequest(http.MethodPut, vol+"/chapters/"+itoa(c.closed)+"/closed",
			strings.NewReader(`{"closed": true, "reason": "сторож: письмо наследников"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := put(f.editorTok); code != http.StatusForbidden {
		t.Fatalf("редактор закрывает главу: %d, ждали 403", code)
	}
	if code := put(f.adminTok); code != http.StatusOK {
		t.Fatalf("администратор закрывает главу: %d, ждали 200", code)
	}
	if err := cache.Put(parentKey, stale); err != nil {
		t.Fatal(err)
	}
}

// checkClosedChapter — закрытая подглава открытого тома: аноним и читатель
// получают том, соседку и родителя без закрытых полос (общая с соседкой —
// тоже), сотрудник видит маркер. Прочее (маркер ни в одном ответе, 404 на
// прямые адреса) проверяет общий обход маршрутов.
func checkClosedChapter(t *testing.T, router http.Handler, f *closedSweep, cache *pagecache.Store) {
	t.Helper()
	c := f.ch
	vol := "/api/works/" + itoa(c.work)
	chPages := func(id int64) string { return vol + "/chapters/" + itoa(id) + "/pages" }
	const sharedText = "Конец речи соседки"
	type want struct {
		code       int
		has, hasNo []string
	}
	outsider := map[string]want{
		vol:                         {200, []string{"Том с прениями"}, nil},
		vol + "/pages":              {200, []string{"Открытая речь соседки", "Открытое заключение"}, []string{sharedText}},
		vol + "/page-map":           {200, []string{`"page_number":4`}, []string{`"page_number":2`, `"page_number":3`}},
		vol + "/reading":            {200, []string{"Открытая речь соседки", "Открытое заключение"}, []string{sharedText}},
		vol + "/chapters":           {200, []string{"Речь соседки", "Прения по докладу"}, nil},
		chPages(c.neighbour):        {200, []string{"Открытая речь соседки"}, []string{sharedText}},
		chPages(c.parent):           {200, []string{"Открытая речь соседки", "Открытое заключение"}, []string{sharedText}},
		vol + "/download?format=md": {200, []string{"Открытое заключение"}, []string{sharedText}},
		vol + "/chapters/" + itoa(c.parent) + "/download?format=md": {200, []string{"Открытое заключение"}, []string{sharedText}},
		vol + "/chapters/" + itoa(c.neighbour) + "/events":          {200, []string{c.eventMixed}, nil},
		vol + "/audio": {200, []string{"Заключение прений"}, nil},
		"/api/audio/" + itoa(c.openTrack) + ".opus":         {302, nil, nil},
		"/api/organizations/" + f.orgA + "/" + c.eventMixed: {200, []string{"Открытая речь"}, []string{"Строка подглавы"}},
		"/api/shelf": {200, []string{f.orgA}, []string{c.org}},
		"/api/editions/" + itoa(f.openEd) + "/highlights":                  {200, nil, nil},
		"/api/collections/podborka":                                        {200, []string{"Прения по докладу"}, nil},
		"/api/collections/podborka/items/" + itoa(c.parentItem) + "/pages": {200, []string{"Открытое заключение"}, []string{sharedText}},
		"/api/collections/podborka/items/" + itoa(c.staffItem) + "/pages":  {410, nil, nil},
		"/api/collections/podborka/download?format=md":                     {200, []string{"Открытое заключение"}, []string{sharedText}},
		"/api/documents/razbor/view":                                       {200, []string{"Текст разбора"}, []string{sharedText}},
		// Догрузка вклейки целиком: с закрытой и с общей полосы — 404 (в
		// чужом разборе та же вклейка — 410 «нет в этом разборе», общий обход).
		"/api/documents/razbor/cuts/" + itoa(c.cuts[0]): {404, nil, nil},
		"/api/documents/razbor/cuts/" + itoa(c.cuts[1]): {404, nil, nil},
		"/api/concepts/" + f.openConcept:                {200, nil, nil},
		"/api/search/pages?q=" + url.QueryEscape("сокровенном") + "&work_id=" + itoa(c.work): {200,
			[]string{`"page_number":1`}, []string{`"page_number":2`, `"page_number":3`}},
		// Номер открытого журнала с закрытой статьёй.
		"/api/works/" + itoa(c.issue) + "/chapters":                               {200, []string{"Открытая статья"}, nil},
		"/api/works/" + itoa(c.issue) + "/pages":                                  {200, []string{"Открытая статья."}, nil},
		"/api/works/" + itoa(c.issue) + "/download?format=md":                     {200, []string{"Открытая статья."}, nil},
		"/api/works/" + itoa(c.issue) + "/chapters/" + itoa(c.article) + "/pages": {404, nil, nil},
		"/api/journals/" + f.openJournal:                                          {200, nil, nil},
		"/api/persons/" + f.openPerson:                                            {200, []string{"Открытая статья"}, nil},
	}
	for _, who := range []struct{ name, token string }{{"аноним", ""}, {"читатель", f.readerTok}} {
		for path, w := range outsider {
			res := sweepDo(t, router, path, who.token)
			if res.code != w.code {
				t.Errorf("закрытая глава, %s: %s отвечает %d, ждали %d", who.name, path, res.code, w.code)
			}
			if leak := leakIn(f, res.text); leak != "" {
				t.Errorf("закрытая глава: %s — %s", path, leak)
			}
			for _, s := range w.has {
				if !strings.Contains(res.text, s) {
					t.Errorf("закрытая глава: %s не отдаёт открытого %q", path, s)
				}
			}
			for _, s := range w.hasNo {
				if strings.Contains(res.text, s) {
					t.Errorf("закрытая глава: %s отдаёт %q — закрытое (общую полосу, закрытую цель, организацию)", path, s)
				}
			}
		}
	}
	// Мои правки: правка на открытой полосе на месте, на закрытой — нет.
	res := sweepDo(t, router, "/api/suggestions/mine", f.readerTok)
	if res.code != http.StatusOK || !strings.Contains(res.text, fmt.Sprintf(`"id":%d,`, c.openSuggestion)) {
		t.Errorf("мои правки (%d): нет правки на открытой полосе — проверка ничего не значит", res.code)
	}
	if strings.Contains(res.text, fmt.Sprintf(`"page_id":%d,`, c.pages[3])) {
		t.Errorf("мои правки: правка на закрытой полосе %d видна читателю", c.pages[3])
	}

	// Сотрудник видит именно маркер подглавы, и его чтение закрытого в кэш не
	// ложится.
	for _, path := range []string{
		chPages(c.closed), chPages(c.parent), chPages(c.grandchild),
		vol + "/pages/" + itoa(c.pages[3]),
		"/api/search?q=" + url.QueryEscape("ЗАКРЫТО"),
		"/api/search/pages?q=" + url.QueryEscape("сокровенном") + "&work_id=" + itoa(c.work),
		"/api/organizations/" + c.org, "/api/organizations/" + c.org + "/" + c.event,
		"/api/organizations/" + f.orgA + "/" + c.eventMixed,
		"/api/concepts/" + c.concept, "/api/persons/" + c.person,
		"/api/editions/" + itoa(f.openEd) + "/highlights",
		"/api/works/" + itoa(c.issue) + "/chapters",
		vol + "/download?format=md",
	} {
		res := sweepDo(t, router, path, f.editorTok)
		if res.code != http.StatusOK || chapterLeak.FindString(res.text) == "" {
			t.Errorf("сотрудник: %s (%d) не видит закрытой подглавы — фикстура ничего не доказывает", path, res.code)
		}
	}
	if res := sweepDo(t, router, vol, f.editorTok); !strings.Contains(res.text, c.hiddenKeys()[0]) {
		t.Errorf("сотрудник: оригинал тома с закрытой главой не выдан (%d) — проверка анонима ничего не значит", res.code)
	}
	for path, key := range map[string]string{
		"/api/audio/" + itoa(c.track) + ".opus":     c.hiddenKeys()[3],
		"/api/audio/rec/" + itoa(c.parentRecording): c.hiddenKeys()[6],
	} {
		if res := sweepDo(t, router, path, f.editorTok); res.code != http.StatusFound || !strings.Contains(res.loc, key) {
			t.Errorf("сотрудник: %s (%d, %q) — ждали 302 на %s", path, res.code, res.loc, key)
		}
	}
	if rc, _, ok := cache.Open(pagecache.Key{WorkID: c.work, Start: 2, End: 3}); ok {
		rc.Close()
		t.Error("чтение закрытой подглавы сотрудником легло в файловый кэш")
	}
}

// sweepTokenless — каналы без токена: прямые адреса закрытого отвечают
// отказом, а обход по ссылкам от главной краулера, llms.txt, карты сайта и
// корня OPDS не встречает закрытого ни в одном ответе и ни разу не приводит
// на адрес закрытого (ссылка на него — уже утечка).
func sweepTokenless(t *testing.T, router http.Handler, f *closedSweep, prefixes []string) {
	t.Helper()
	gone := map[string]int{}
	for _, w := range f.closedWorks() {
		id, ch := itoa(w), itoa(f.chapters[w])
		gone["/seo/works/"+id] = http.StatusGone
		gone["/seo/works/"+id+"/chapters/"+ch] = http.StatusGone
		gone["/seo/works/"+id+"/pages/1"] = http.StatusGone
		gone["/seo/works/"+id+".md"] = http.StatusGone
		gone["/seo/works/"+id+"/chapters/"+ch+".md"] = http.StatusGone
		gone["/og/work/"+id+".png"] = http.StatusNotFound
		gone["/og/chapter/"+ch+".png"] = http.StatusNotFound
		gone["/opds/works/"+id] = http.StatusNotFound
	}
	gone["/seo/editions/"+itoa(f.closedEd)] = http.StatusGone
	gone["/og/edition/"+itoa(f.closedEd)+".png"] = http.StatusNotFound
	gone["/opds/editions/"+itoa(f.closedEd)] = http.StatusNotFound
	// Закрытая подглава открытого тома и её потомок: 410 на странице и в
	// тексте, карточки нет, в OPDS узла нет; закрытые полосы (и общая с
	// соседкой) — 410.
	cv := itoa(f.ch.work)
	for _, ch := range []int64{f.ch.closed, f.ch.grandchild} {
		gone["/seo/works/"+cv+"/chapters/"+itoa(ch)] = http.StatusGone
		gone["/seo/works/"+cv+"/chapters/"+itoa(ch)+".md"] = http.StatusGone
		gone["/og/chapter/"+itoa(ch)+".png"] = http.StatusNotFound
		gone["/opds/works/"+cv+"/chapters/"+itoa(ch)] = http.StatusNotFound
	}
	gone["/seo/works/"+cv+"/pages/2"] = http.StatusGone
	gone["/seo/works/"+cv+"/pages/3"] = http.StatusGone
	for path, want := range gone {
		res := sweepDo(t, router, path, "")
		if res.code != want {
			t.Errorf("бестокенный: %s отвечает %d, ждали %d", path, res.code, want)
		}
		if leak := leakIn(f, res.text); leak != "" {
			t.Errorf("бестокенный: %s — %s", path, leak)
		}
	}

	seeds := []string{"/seo", "/seo/llms.txt", "/seo/concepts", "/seo/concepts.md", "/seo/collections",
		"/seo/documents", "/seo/works/" + itoa(f.open), "/seo/works/" + itoa(f.open) + ".md",
		"/seo/editions/" + itoa(f.openEd), "/seo/concepts/" + f.openConcept, "/seo/concepts/" + f.openConcept + ".md",
		"/seo/concepts/" + f.closedConcept, "/seo/concepts/" + f.closedConcept + ".md",
		"/seo/collections/podborka", "/seo/collections/" + f.readerNick + "/podborka",
		"/seo/documents/razbor", "/seo/documents/" + f.readerNick + "/razbor",
		"/robots.txt", "/sitemap.xml", "/opds", "/opds/search?q=" + url.QueryEscape("ЗАКРЫТО"),
		"/opds/search?q=" + url.QueryEscape("сокровенном"),
		"/seo/works/" + cv, "/seo/works/" + cv + ".md", "/opds/works/" + cv,
		"/seo/works/" + cv + "/chapters/" + itoa(f.ch.neighbour), "/seo/works/" + cv + "/chapters/" + itoa(f.ch.parent) + ".md",
		"/seo/concepts/" + f.ch.concept, "/seo/concepts/" + f.ch.concept + ".md"}
	// Каждый канал-префикс роутера — хотя бы голым адресом: новый канал
	// попадает в обход сам, без правки списка выше.
	seeds = append(seeds, prefixes...)
	// ownChannel — адрес, который бэкенд обслуживает сам (канал-префикс,
	// API, служебные файлы), а не страница читальни под /seo.
	ownChannel := func(raw string) bool {
		for _, p := range append([]string{"/robots.txt", "/sitemap.xml", "/api/"}, prefixes...) {
			if strings.HasPrefix(raw, p) {
				return true
			}
		}
		return false
	}
	link := regexp.MustCompile(`(?:href|src)="([^"]+)"|<loc>([^<]+)</loc>|\]\((` + regexp.QuoteMeta(sweepBase) + `[^)\s]*)\)`)
	seen := map[string]bool{}
	type visit struct {
		path, from string
	}
	queue := []visit{}
	for _, s := range seeds {
		queue = append(queue, visit{s, ""})
	}
	visited := 0
	for len(queue) > 0 && visited < 1500 {
		v := queue[0]
		queue = queue[1:]
		if seen[v.path] {
			continue
		}
		seen[v.path] = true
		visited++
		res := sweepDo(t, router, v.path, "")
		if leak := leakIn(f, res.text); leak != "" {
			t.Errorf("обход каналов: %s (%d, пришли с %s) — %s", v.path, res.code, v.from, leak)
		}
		if res.code == http.StatusGone && v.from != "" {
			t.Errorf("обход каналов: %s ссылается на снятое или закрытое %s", v.from, v.path)
		}
		next := func(raw string) {
			raw = strings.ReplaceAll(raw, "&amp;", "&")
			raw = strings.Replace(raw, "{searchTerms}", url.QueryEscape("сокровенном"), 1)
			if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "#") {
				return
			}
			if strings.HasPrefix(raw, "http") {
				if !strings.HasPrefix(raw, sweepBase) {
					return
				}
				raw = strings.TrimPrefix(raw, sweepBase)
			}
			if !strings.HasPrefix(raw, "/") {
				return
			}
			if i := strings.Index(raw, "#"); i >= 0 {
				raw = raw[:i]
			}
			switch {
			case raw == "" || raw == "/":
				raw = "/seo"
			case strings.HasPrefix(raw, "/mcp"):
				return
			case !ownChannel(raw):
				// Адрес читальни: бэкенд знает его страницей краулера.
				raw = "/seo" + raw
			}
			if !seen[raw] {
				queue = append(queue, visit{raw, v.path})
			}
		}
		if res.code == http.StatusMovedPermanently || res.code == http.StatusFound {
			next(res.loc)
			continue
		}
		if res.code != http.StatusOK || strings.HasPrefix(v.path, "/og/") {
			continue
		}
		for _, m := range link.FindAllStringSubmatch(res.text, -1) {
			for _, g := range m[1:] {
				if g != "" {
					next(g)
				}
			}
		}
	}
	t.Logf("обход бестокенных каналов: %d адресов", visited)
	if visited < 40 {
		t.Errorf("обход бестокенных каналов подозрительно мал: %d адресов", visited)
	}
	// Обход дошёл до тома с закрытой подглавой по ссылкам, а не только по
	// затравкам: из карты сайта и llms.txt — до соседки, из OPDS — до
	// выгрузки тома (FB2 — текстом, его маркер виден).
	reached := func(what string, ok func(string) bool) {
		for p := range seen {
			if ok(p) {
				return
			}
		}
		t.Errorf("обход бестокенных каналов не дошёл до %s", what)
	}
	reached("соседки по ссылке", func(p string) bool {
		return strings.Contains(p, "/chapters/"+itoa(f.ch.neighbour)+"-") && !strings.HasSuffix(p, ".md")
	})
	reached("текста тома (.md) по ссылке", func(p string) bool {
		return strings.HasPrefix(p, "/seo/works/"+cv+"-") && strings.HasSuffix(p, ".md")
	})
	reached("выгрузки тома из OPDS", func(p string) bool {
		return strings.HasPrefix(p, "/api/works/"+cv+"/download?format=fb2")
	})
	reached("выгрузки родителя из OPDS", func(p string) bool {
		return strings.HasPrefix(p, "/api/works/"+cv+"/chapters/"+itoa(f.ch.parent)+"/download?format=fb2")
	})
}

// sweepMCP — MCP через тот же роутер: поиск, полосы тома и тексты по
// адресам закрытого не выдают закрытого.
func sweepMCP(t *testing.T, router http.Handler, f *closedSweep) {
	t.Helper()
	srv := httptest.NewServer(router)
	defer srv.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "sweep", Version: "1"}, nil).
		Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: srv.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	type call struct {
		tool string
		args map[string]any
	}
	calls := []call{
		{"search", map[string]any{"query": "ЗАКРЫТО"}},
		{"search", map[string]any{"query": "сокровенном"}},
		{"fetch", map[string]any{"id": "/"}},
		{"fetch", map[string]any{"id": "/concepts"}},
		{"fetch", map[string]any{"id": "/concepts/" + f.closedConcept}},
		{"fetch", map[string]any{"id": "/concepts/" + f.openConcept}},
		{"fetch", map[string]any{"id": "/concepts/" + f.ch.concept}},
		{"search_volume", map[string]any{"query": "сокровенном", "work": f.ch.work}},
		{"search_volume", map[string]any{"query": "ЗАКРЫТО", "work": f.ch.work}},
	}
	cv := "/works/" + itoa(f.ch.work)
	calls = append(calls, call{"fetch", map[string]any{"id": cv}},
		call{"fetch", map[string]any{"id": cv + "/pages/1-4"}},
		call{"fetch", map[string]any{"id": cv + "/pages/2-3"}},
		// Края диапазона на закрытой полосе: шапка называет главы краёв.
		call{"fetch", map[string]any{"id": cv + "/pages/3-4"}},
		call{"fetch", map[string]any{"id": cv + "/pages/1-3"}})
	for _, ch := range []int64{f.ch.parent, f.ch.neighbour, f.ch.closed, f.ch.grandchild} {
		calls = append(calls, call{"fetch", map[string]any{"id": cv + "/chapters/" + itoa(ch)}})
	}
	for _, w := range f.closedWorks() {
		id := "/works/" + itoa(w)
		calls = append(calls,
			call{"search_volume", map[string]any{"query": "сокровенном", "work": w}},
			call{"fetch", map[string]any{"id": id}},
			call{"fetch", map[string]any{"id": id + "/chapters/" + itoa(f.chapters[w])}},
			call{"fetch", map[string]any{"id": id + "/pages/1-2"}})
	}
	opened := 0
	for _, c := range calls {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: c.tool, Arguments: c.args})
		if err != nil {
			t.Fatalf("MCP %s %v: %v", c.tool, c.args, err)
		}
		var b strings.Builder
		for _, part := range res.Content {
			if tc, ok := part.(*sdk.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		if leak := leakIn(f, b.String()); leak != "" {
			t.Errorf("MCP %s %v — %s", c.tool, c.args, leak)
		}
		if strings.Contains(b.String(), "Открыт") {
			opened++
		}
	}
	if opened == 0 {
		t.Error("MCP ни разу не отдал открытого — проверка ничего не значит")
	}
}

// TestLeakInDecodesAroundStrayPercent — одиночный «%» в теле не отключает
// проверку закодированного маркера: раскодирование идёт кусками %XX.
func TestLeakInDecodesAroundStrayPercent(t *testing.T) {
	f := &closedSweep{}
	body := "скидка 50% на всё, ссылка /works/1-" + url.QueryEscape(closedMarker)
	if leakIn(f, body) == "" {
		t.Errorf("закодированный маркер рядом с «50%%» не найден: %s", body)
	}
	if leak := leakIn(f, "скидка 50% и %zz, ничего закрытого"); leak != "" {
		t.Errorf("ложная находка: %s", leak)
	}
}
