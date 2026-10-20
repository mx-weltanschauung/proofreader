package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// Сторож условной нумерации томов (миграция 000039). У собрания с
// volume_numbering = 'order' номер тома ставит книги по порядку, но ни один
// запрос, из которого собирается подпись для читателя, не отдаёт его как
// печатаемый: признак у работы и сводки полки, пустая координата у избранного
// и источника подборки, пустая подпись тома в поиске, слаг без «t24». Та же
// фикстура с печатной нумерацией обязана отдавать номер как раньше — иначе
// тест проходил бы и на запросе, гасящем номер всем подряд.
//
// Без PROOFREADER_TEST_DB_URL тест пропускается.

type numberingFixture struct {
	pool      *pgxpool.Pool
	editions  *EditionRepository
	works     *WorkRepository
	edition   *models.Edition
	volume    *models.Work
	front     int64
	preface   int64
	chapter   int64
	numbering string
}

func newNumberingFixture(t *testing.T, numbering, slug string) numberingFixture {
	t.Helper()
	ctx := context.Background()
	pool := testPool(t)
	f := numberingFixture{
		pool: pool, editions: NewEditionRepository(pool), works: NewWorkRepository(pool),
		numbering: numbering,
	}
	f.edition = &models.Edition{Title: "Стенограммы " + slug, Slug: slug, VolumeNumbering: numbering}
	if err := f.editions.Create(ctx, f.edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	f.volume = newVolume(t, f.works, "XII пленум ИККИ "+slug)
	number := 24
	f.volume.EditionID = &f.edition.ID
	f.volume.VolumeNumber = &number
	if err := f.works.Update(ctx, f.volume); err != nil {
		t.Fatalf("attach volume: %v", err)
	}
	seedPages(t, pool, f.volume.ID, "доклад о тактике единого фронта", "прения")
	f.chapter = seedChapter(t, pool, f.volume.ID, "Доклад о тактике", 1, 2)
	f.front = seedChildWorkWithPages(t, pool, f.volume.ID, "Титул "+slug, "титул")
	// Предисловие к собранию, стоящее перед томом 24: его слаг строится из
	// precedes_volume («…-front-t24»), а не из номера тома.
	preface := &models.Work{
		Title: "От редакции " + slug, Author: "проба", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleEditionFrontMatter,
		EditionID: &f.edition.ID, PrecedesVolume: &number,
		NumberingStyle: models.NumberingArabic, OwnerID: 1,
	}
	if err := f.works.Create(ctx, preface); err != nil {
		t.Fatalf("create preface: %v", err)
	}
	t.Cleanup(func() { _ = f.works.Delete(context.Background(), preface.ID) })
	f.preface = preface.ID
	return f
}

func TestOrderOnlyNumberingHidesVolumeNumber(t *testing.T) {
	ctx := context.Background()
	order := newNumberingFixture(t, models.VolumeNumberingOrder, "ki-order")
	printed := newNumberingFixture(t, models.VolumeNumberingPrinted, "ki-printed")

	for _, f := range []numberingFixture{order, printed} {
		hidden := f.numbering == models.VolumeNumberingOrder
		t.Run(f.numbering, func(t *testing.T) {
			ed, err := f.editions.GetByID(ctx, f.edition.ID)
			if err != nil {
				t.Fatalf("get edition: %v", err)
			}
			if ed.VolumeNumbering != f.numbering {
				t.Fatalf("editions.volume_numbering = %q, ожидалось %q", ed.VolumeNumbering, f.numbering)
			}

			// Строка работы: признак, номер для полки на месте, слаг.
			w, err := f.works.GetByID(ctx, f.volume.ID)
			if err != nil {
				t.Fatalf("get work: %v", err)
			}
			if w.VolumeOrderOnly != hidden {
				t.Errorf("Work.VolumeOrderOnly = %v, ожидалось %v", w.VolumeOrderOnly, hidden)
			}
			if w.VolumeNumber == nil || *w.VolumeNumber != 24 {
				t.Errorf("номер тома для полки пропал: %v", w.VolumeNumber)
			}
			if got := strings.Contains(w.Slug, "t24"); got == hidden {
				t.Errorf("слаг %q: номер в нём = %v, ожидалось %v", w.Slug, got, !hidden)
			}
			front, err := f.works.GetByID(ctx, f.front)
			if err != nil {
				t.Fatalf("get front matter: %v", err)
			}
			if front.VolumeOrderOnly != hidden {
				t.Errorf("передние листы: VolumeOrderOnly = %v, ожидалось %v (собрание родителя)",
					front.VolumeOrderOnly, hidden)
			}
			if got := strings.Contains(front.Slug, "t24"); got == hidden {
				t.Errorf("слаг передних листов %q: номер в нём = %v, ожидалось %v", front.Slug, got, !hidden)
			}

			pre, err := f.works.GetByID(ctx, f.preface)
			if err != nil {
				t.Fatalf("get preface: %v", err)
			}
			if got := strings.Contains(pre.Slug, "t24"); got == hidden {
				t.Errorf("слаг предисловия %q: номер в нём = %v, ожидалось %v", pre.Slug, got, !hidden)
			}

			// Список томов собрания (staticsite, OPDS, краулер).
			listed, err := f.editions.ListWorks(ctx, f.edition.ID)
			if err != nil || len(listed) != 2 {
				t.Fatalf("list works: %v, %d", err, len(listed))
			}
			for _, lw := range listed {
				if lw.VolumeOrderOnly != hidden {
					t.Errorf("ListWorks %q: VolumeOrderOnly = %v, ожидалось %v", lw.Title, lw.VolumeOrderOnly, hidden)
				}
			}

			// Сводки полки — страница собрания и главная.
			sums, err := f.editions.ListWorkSummaries(ctx, f.edition.ID)
			if err != nil || len(sums) != 2 {
				t.Fatalf("summaries: %v, %d", err, len(sums))
			}
			// Предисловие идёт первым (role <> 'edition_front_matter'), том — вторым.
			vol := sums[1]
			if vol.ID != f.volume.ID {
				t.Fatalf("порядок сводок: %d, ожидался том %d", vol.ID, f.volume.ID)
			}
			if vol.VolumeOrderOnly != hidden {
				t.Errorf("сводка полки: VolumeOrderOnly = %v, ожидалось %v", vol.VolumeOrderOnly, hidden)
			}
			if vol.VolumeNumber == nil || *vol.VolumeNumber != 24 {
				t.Errorf("сводка полки: номер для порядка пропал: %v", vol.VolumeNumber)
			}
			all, err := f.editions.ListAllWorkSummaries(ctx)
			if err != nil {
				t.Fatalf("all summaries: %v", err)
			}
			for _, s := range all {
				if s.ID == f.volume.ID && s.VolumeOrderOnly != hidden {
					t.Errorf("главная: VolumeOrderOnly = %v, ожидалось %v", s.VolumeOrderOnly, hidden)
				}
			}

			// Избранное собрания: координата «т. 24».
			if err := f.editions.ReplaceHighlights(ctx, f.edition.ID,
				[]models.HighlightInput{{ChapterID: f.chapter}}); err != nil {
				t.Fatalf("highlights: %v", err)
			}
			hs, err := f.editions.ListHighlights(ctx, f.edition.ID)
			if err != nil || len(hs) != 1 {
				t.Fatalf("list highlights: %v, %d", err, len(hs))
			}
			if got := hs[0].VolumeNumber != nil; got == hidden {
				t.Errorf("избранное: номер тома отдан = %v, ожидалось %v", got, !hidden)
			}

			// Источник элемента подборки: «т. 24, с. 1—2».
			var collectionID int64
			if err := f.pool.QueryRow(ctx, `
				INSERT INTO collections (title, slug, description) VALUES ('п', $1, '')
				RETURNING id`, "pod-"+f.numbering).Scan(&collectionID); err != nil {
				t.Fatalf("seed collection: %v", err)
			}
			if _, err := f.pool.Exec(ctx, `
				INSERT INTO collection_items (collection_id, kind, chapter_id, work_id,
				                              snapshot_title, snapshot_author, order_number)
				VALUES ($1, 'chapter', $2, $3, 'Доклад', '', 1)`,
				collectionID, f.chapter, f.volume.ID); err != nil {
				t.Fatalf("seed collection item: %v", err)
			}
			rows, err := NewCollectionRepository(f.pool).ItemRows(ctx, collectionID)
			if err != nil || len(rows) != 1 {
				t.Fatalf("item rows: %v, %d", err, len(rows))
			}
			if got := rows[0].VolumeNumber != nil; got == hidden {
				t.Errorf("источник подборки: номер тома отдан = %v, ожидалось %v", got, !hidden)
			}

			// Поиск: подпись тома в каталоге глав и в списке томов (её же
			// печатает MCP).
			res, err := NewSearchRepository(f.pool).Search(ctx, models.SearchQuery{
				Text: "тактике", EditionIDs: []int64{f.edition.ID},
			})
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(res.Volumes) == 0 || len(res.Chapters) == 0 {
				t.Fatalf("поиск ничего не нашёл: %d томов, %d глав", len(res.Volumes), len(res.Chapters))
			}
			for _, v := range res.Volumes {
				if got := strings.Contains(v.VolumeLabel, "24"); got == hidden {
					t.Errorf("поиск, том: подпись %q, номер в ней = %v, ожидалось %v", v.VolumeLabel, got, !hidden)
				}
			}
			for _, c := range res.Chapters {
				if got := strings.Contains(c.VolumeLabel, "24"); got == hidden {
					t.Errorf("поиск, глава: подпись %q, номер в ней = %v, ожидалось %v", c.VolumeLabel, got, !hidden)
				}
			}
		})
	}
}

// Правка собрания пишет нумерацию и не теряет её на круге чтения.
func TestEditionVolumeNumberingRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewEditionRepository(testPool(t))
	e := &models.Edition{Title: "Собрание", Slug: "sobranie"}
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.GetByID(ctx, e.ID)
	if err != nil || got.VolumeNumbering != models.VolumeNumberingPrinted {
		t.Fatalf("умолчание: %v, %q", err, got.VolumeNumbering)
	}
	got.VolumeNumbering = models.VolumeNumberingOrder
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, err := repo.List(ctx)
	if err != nil || len(list) != 1 || list[0].VolumeNumbering != models.VolumeNumberingOrder {
		t.Fatalf("после правки: %v, %+v", err, list)
	}
	if _, err := repo.pool.Exec(ctx, `UPDATE editions SET volume_numbering = 'roman' WHERE id = $1`, e.ID); err == nil {
		t.Fatal("CHECK пропустил значение вне перечня")
	}
}
