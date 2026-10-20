package repository

import (
	"context"
	"reflect"
	"testing"
)

// Закрытое в указателе: понятия, статьи и адреса закрытых изданий и томов.
// Фикстура — издание 1 (открыто, т. 4 = работа 10 открыта, т. 5 = работа 11
// закрыта), издание 2 закрыто (работа 20).
//
//	a: статья в изд. 1 с текстом и адресами в т. 4 и т. 5 + статья в изд. 2
//	b: одна статья, изд. 2                     → скрыто целиком
//	c: статья изд. 1 без текста, адрес в т. 5  → пустая оболочка, скрыто
//	d: статья изд. 1 без текста, адрес в т. 4  → видно
//	e: статья изд. 1, её работа-указатель (11) закрыта → скрыто
func seedClosedIndex(t *testing.T) (*IndexRepository, *SEORepository) {
	t.Helper()
	repo, pool, ctx := newSEOTestRepo(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO editions (id, title, slug, closed) VALUES
		  (1, 'Открытое', 'open', false), (2, 'Закрытое', 'closed', true);
		INSERT INTO works (id, title, author, file_path, owner_id, role, edition_id, volume_number, closed) VALUES
		  (10, 'Том 4', 'А', 'f', 1, 'volume', 1, 4, false),
		  (11, 'Том 5', 'А', 'f', 1, 'volume', 1, 5, true),
		  (20, 'Том 1', 'А', 'f', 1, 'volume', 2, 1, false);
		INSERT INTO index_concepts (id, title, slug, sort_key, title_key) VALUES
		  (1, 'А', 'a', 'a', 'a'), (2, 'Б', 'b', 'b', 'b'), (3, 'В', 'c', 'c', 'c'), (4, 'Г', 'd', 'd', 'd'), (5, 'Д', 'e', 'e', 'e');
		INSERT INTO index_concept_articles (id, concept_id, edition_id, work_id, title, title_key, kind, article_markdown) VALUES
		  (1, 1, 1, NULL, 'А', 'a', 'article', 'текст'),
		  (2, 1, 2, NULL, 'А', 'a', 'article', 'закрытая'),
		  (3, 2, 2, NULL, 'Б', 'b', 'article', 'закрытая'),
		  (4, 3, 1, NULL, 'В', 'c', 'article', ''),
		  (5, 4, 1, NULL, 'Г', 'd', 'article', ''),
		  (6, 5, 1, 11, 'Д', 'e', 'article', 'работа-указатель закрыта');
		INSERT INTO index_references (article_id, volume_number, page_start, page_end, order_number) VALUES
		  (1, 4, 1, 1, 1), (1, 5, 2, 2, 2), (2, 1, 3, 3, 1), (3, 1, 3, 3, 1),
		  (4, 5, 2, 2, 1), (5, 4, 1, 1, 1)`); err != nil {
		t.Fatalf("подготовка данных: %v", err)
	}
	return NewIndexRepository(pool), repo
}

func slugsOf(t *testing.T, repo *IndexRepository, includeClosed bool) []string {
	t.Helper()
	cs, err := repo.ListConcepts(context.Background(), "", "", 100, 0, includeClosed)
	if err != nil {
		t.Fatalf("ListConcepts: %v", err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.Slug)
	}
	return out
}

func TestListConceptsHidesClosedUnlessIncluded(t *testing.T) {
	idx, _ := seedClosedIndex(t)
	if got, want := slugsOf(t, idx, false), []string{"a", "d"}; !reflect.DeepEqual(got, want) {
		t.Errorf("для не-сотрудника %v, ждали %v", got, want)
	}
	if got, want := slugsOf(t, idx, true), []string{"a", "b", "c", "d", "e"}; !reflect.DeepEqual(got, want) {
		t.Errorf("для сотрудника %v, ждали %v", got, want)
	}
}

func TestListConceptsEditionIDsOmitClosedEdition(t *testing.T) {
	idx, _ := seedClosedIndex(t)
	for includeClosed, want := range map[bool][]int64{false: {1}, true: {1, 2}} {
		cs, err := idx.ListConcepts(context.Background(), "", "a", 100, 0, includeClosed)
		if err != nil {
			t.Fatal(err)
		}
		if len(cs) != 1 || !reflect.DeepEqual(cs[0].EditionIDs, want) {
			t.Errorf("includeClosed=%v: edition_ids %+v, ждали %v", includeClosed, cs, want)
		}
	}
}

func TestSEOConceptsAndShelfHideClosedAlways(t *testing.T) {
	_, seoRepo := seedClosedIndex(t)
	rows, err := seoRepo.Concepts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, r := range rows {
		slugs = append(slugs, r.Slug)
	}
	if want := []string{"a", "d"}; !reflect.DeepEqual(slugs, want) {
		t.Errorf("Concepts: %v, ждали %v", slugs, want)
	}
	shelf, err := seoRepo.ConceptShelf(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, r := range shelf {
		got[r.Slug] = r.Places
	}
	// «a»: из трёх адресов виден один (т. 4); адреса закрытых т. 5 и изд. 2 не считаются.
	if want := map[string]int{"a": 1, "d": 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("ConceptShelf: %v, ждали %v", got, want)
	}
}
