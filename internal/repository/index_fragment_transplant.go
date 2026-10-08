package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// refKey — то, чем адрес назван в ПЕЧАТНОМ указателе: том, часть, границы
// полос и подрубрика, под которой он стоит.
//
// Идентификатор адреса ключом быть не может: ввоз издания пересоздаёт адреса
// целиком, и после переимпорта ни один старый id не существует. Ключ пережил
// переимпорт — вырезка находит своё место; не пережил (подрубрику
// переименовали, адрес исчез из указателя) — вырезка называется вслух.
type refKey struct {
	Volume    int
	Part      string
	PageStart int
	PageEnd   int
	RubricKey string
}

// rescuedFragment — вырезка, снятая с адреса перед его сносом, вместе с
// ключом адреса и заголовком понятия для предупреждения.
type rescuedFragment struct {
	ArticleID    int64
	ConceptTitle string
	Key          refKey
	// TitlePath — тот же путь подрубрик, что и в Key.RubricKey, но
	// заголовками, как они напечатаны, а не нормализованными ключами.
	// Держится ОТДЕЛЬНО от refKey намеренно: refKey — ключ сопоставления
	// (сравнивается как есть, обязан быть нормализован), а TitlePath нужен
	// только человеку — в предупреждении о потерянной вырезке (см.
	// transplantFragments). До круга правок 1 задачи 8 предупреждение
	// печатало RubricKey — NUL-склеенные нормализованные ключи вида
	// "родитель\x00лист" — читателю показывалась не строка, а сырой ключ.
	TitlePath   []string
	OrderNumber int
	StartPageID int64
	StartOffset int
	EndPageID   int64
	EndOffset   int
	HeadQuote   string
	TailQuote   string
	StartHash   string
	EndHash     string
	Status      string
}

// rescueFragments снимает вырезки статьи ДО сноса её адресов.
//
// Зовётся только для статьи, которая в новом наборе ЕСТЬ. Статья, исчезнувшая
// из указателя целиком, вырезки уносит с собой, и спасать их некуда — адресов
// у неё после сноса не остаётся ни в каком виде; такая потеря считается и
// называется отдельно, в шаге 1 ReplaceForEdition.
func rescueFragments(ctx context.Context, tx pgx.Tx, articleID int64, conceptTitle string) ([]rescuedFragment, error) {
	rows, err := tx.Query(ctx, rubricAncestryByArticleCTE+`
		SELECT r.volume_number, COALESCE(r.volume_part, ''), r.page_start, r.page_end,
		       COALESCE(anc.key_path, ARRAY[]::text[]), COALESCE(anc.path, ARRAY[]::text[]),
		       f.order_number, f.start_page_id, f.start_offset, f.end_page_id,
		       f.end_offset, f.head_quote, f.tail_quote, f.start_hash, f.end_hash,
		       f.status
		FROM index_fragments f
		JOIN index_references r ON r.id = f.reference_id
		LEFT JOIN rubric_ancestry anc ON anc.id = r.rubric_id
		WHERE r.article_id = $1
		ORDER BY f.reference_id, f.order_number
	`, articleID)
	if err != nil {
		return nil, fmt.Errorf("failed to read fragments of %q: %w", conceptTitle, err)
	}
	defer rows.Close()

	var out []rescuedFragment
	for rows.Next() {
		f := rescuedFragment{ArticleID: articleID, ConceptTitle: conceptTitle}
		var keyPath []string
		if err := rows.Scan(&f.Key.Volume, &f.Key.Part, &f.Key.PageStart, &f.Key.PageEnd,
			&keyPath, &f.TitlePath, &f.OrderNumber, &f.StartPageID, &f.StartOffset,
			&f.EndPageID, &f.EndOffset, &f.HeadQuote, &f.TailQuote,
			&f.StartHash, &f.EndHash, &f.Status); err != nil {
			return nil, fmt.Errorf("failed to scan fragment of %q: %w", conceptTitle, err)
		}
		f.Key.RubricKey = rubricPathKey(keyPath)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read fragments of %q: %w", conceptTitle, err)
	}
	return out, nil
}

// transplantFragments сажает снятые вырезки на новые адреса и возвращает
// предупреждения о тех, которым места не нашлось.
//
// Зовётся ПОСЛЕ CopyFrom: адреса к этому моменту уже в таблице, а их
// идентификаторы CopyFrom не возвращает — отсюда обратное чтение. Читается по
// статье, а не по изданию: статей с вырезками единицы, и тащить 199 тысяч
// строк обратно в память ради них незачем.
func transplantFragments(ctx context.Context, tx pgx.Tx, rescued []rescuedFragment) ([]string, error) {
	if len(rescued) == 0 {
		return nil, nil
	}

	byArticle := map[int64]map[refKey]int64{}
	for _, f := range rescued {
		if _, done := byArticle[f.ArticleID]; done {
			continue
		}
		keys, err := newReferenceKeys(ctx, tx, f.ArticleID)
		if err != nil {
			return nil, err
		}
		byArticle[f.ArticleID] = keys
	}

	var warnings []string
	for _, f := range rescued {
		newID, ok := byArticle[f.ArticleID][f.Key]
		if !ok {
			warnings = append(warnings, fmt.Sprintf(
				"понятие %q: вырезка на адрес «том %d, стр. %d—%d», %s, не нашла места после переимпорта и потеряна — подрубрику переименовали или адрес исчез из указателя",
				f.ConceptTitle, f.Key.Volume, f.Key.PageStart, f.Key.PageEnd, rubricParentDescription(f.TitlePath)))
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO index_fragments
				(reference_id, order_number, start_page_id, start_offset,
				 end_page_id, end_offset, head_quote, tail_quote,
				 start_hash, end_hash, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, newID, f.OrderNumber, f.StartPageID, f.StartOffset, f.EndPageID,
			f.EndOffset, f.HeadQuote, f.TailQuote, f.StartHash, f.EndHash,
			f.Status); err != nil {
			return nil, fmt.Errorf("failed to transplant fragment of %q: %w", f.ConceptTitle, err)
		}
	}
	return warnings, nil
}

// newReferenceKeys — карта «ключ адреса → идентификатор» по свежевставленным
// адресам статьи.
func newReferenceKeys(ctx context.Context, tx pgx.Tx, articleID int64) (map[refKey]int64, error) {
	rows, err := tx.Query(ctx, rubricAncestryByArticleCTE+`
		SELECT r.id, r.volume_number, COALESCE(r.volume_part, ''),
		       r.page_start, r.page_end, COALESCE(anc.key_path, ARRAY[]::text[])
		FROM index_references r
		LEFT JOIN rubric_ancestry anc ON anc.id = r.rubric_id
		WHERE r.article_id = $1
		ORDER BY r.id
	`, articleID)
	if err != nil {
		return nil, fmt.Errorf("failed to read new references of article %d: %w", articleID, err)
	}
	defer rows.Close()

	out := map[refKey]int64{}
	for rows.Next() {
		var id int64
		var k refKey
		var keyPath []string
		if err := rows.Scan(&id, &k.Volume, &k.Part, &k.PageStart, &k.PageEnd, &keyPath); err != nil {
			return nil, fmt.Errorf("failed to scan new reference: %w", err)
		}
		k.RubricKey = rubricPathKey(keyPath)
		// Повтор ключа — печатный указатель иногда даёт один адрес дважды.
		// Вырезки такой пары съезжаются на первый: координаты у них
		// одинаковые, и различить, какая была на каком, нечем. Это слияние,
		// а не потеря.
		if _, seen := out[k]; !seen {
			out[k] = id
		}
	}
	return out, rows.Err()
}
