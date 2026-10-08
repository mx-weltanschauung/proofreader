package mcp

import (
	"context"
	"fmt"
	"strings"

	"proofreader/internal/limit"
	"proofreader/internal/models"
	"proofreader/internal/seo"
	"proofreader/pkg/book"
)

type FetchInput struct {
	ID string `json:"id" jsonschema:"адрес из поля id или url результата: /, /works/{том}, /works/{том}/chapters/{глава}[/part-N], /works/{том}/pages/{a}-{b} (до 10 полос), /concepts/{слаг}[/part-N][?rubric_path=…] (статья, подрубрики и текст мест частями; rubric_path — одна подрубрика), /concepts (все понятия)"`
}

// FetchOutput — контракт fetch deep research ChatGPT.
type FetchOutput struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
	URL   string `json:"url"`
}

func (s *service) fetch(ctx context.Context, in FetchInput) (*FetchOutput, error) {
	a, err := ParseID(in.ID)
	if err != nil {
		return nil, err
	}
	switch a.Kind {
	case KindCatalog:
		return s.fetchText(ctx, "/llms.txt")
	case KindWork:
		return s.fetchText(ctx, fmt.Sprintf("/works/%d.md", a.WorkID))
	case KindChapter:
		p := fmt.Sprintf("/works/%d/chapters/%d", a.WorkID, a.ChapterID)
		if a.Part > 1 {
			p += fmt.Sprintf("/part-%d", a.Part)
		}
		return s.fetchText(ctx, p+".md")
	case KindPages:
		return s.fetchPages(ctx, a)
	case KindConcept:
		p := "/concepts/" + a.Slug
		if a.Part > 1 {
			p += fmt.Sprintf("/part-%d", a.Part)
		}
		return s.fetchText(ctx, p+".md"+seo.RubricQuery(a.Rubric))
	default:
		p := "/concepts"
		if a.Part > 1 {
			p += fmt.Sprintf("/part-%d", a.Part)
		}
		return s.fetchText(ctx, p+".md")
	}
}

// heavyAcquire — тяжёлый слот /seo для MCP: сперва свои ворота, потом общий
// слот. wait == true — оба с ожиданием (зовётся вне полёта). wait == false
// не ждёт нигде — ни у ворот MCP, ни у общего слота: seo.Handler.Text зовёт
// так внутри полёта, к которому мог присоединиться краулер, и краулер не
// должен оказаться в очереди за MCP. Отказ — errBusy, ничего не занято.
func (s *service) heavyAcquire(ctx context.Context, heavy limit.Slots, wait bool) (func(), error) {
	if wait {
		rel, ok := limit.Nested(ctx, s.g.heavy, s.g.gateWait, heavy, s.g.sharedWait)
		if !ok {
			// Ушедший клиент — не занятость (как в underSearch): иначе в
			// журнале он попадёт в счёт отказов.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errBusy
		}
		return rel, nil
	}
	relGate, ok := s.g.heavy.TryAcquire()
	if !ok {
		return nil, errBusy
	}
	relShared, ok := heavy.TryAcquire()
	if !ok {
		relGate()
		return nil, errBusy
	}
	return func() {
		relShared()
		relGate()
	}, nil
}

// fetchText — готовый текст /seo по пути: номер вместо слага, канон и кэш —
// забота seo.Handler.Text.
func (s *service) fetchText(ctx context.Context, path string) (*FetchOutput, error) {
	res, err := s.d.Text.Text(ctx, path, s.heavyAcquire)
	if err != nil {
		return nil, err
	}
	// id — путь без «.md», строка запроса (подрубрика) остаётся: модель
	// передаст его обратно и получит тот же текст.
	bare, query, hasQuery := strings.Cut(res.Path, "?")
	id := strings.TrimSuffix(bare, ".md")
	if hasQuery {
		id += "?" + query
	}
	out := &FetchOutput{ID: id, Text: res.Body, URL: res.Canonical, Title: firstLine(res.Body)}
	if res.Path == "/llms.txt" {
		out.ID, out.URL = "/", s.base+"/"
	}
	return out, nil
}

// firstLine — заглавие из первой строки текста, без решёток markdown.
func firstLine(body string) string {
	line, _, _ := strings.Cut(body, "\n")
	return strings.TrimSpace(strings.TrimLeft(line, "# "))
}

func (s *service) work(ctx context.Context, id int64) (*models.Work, error) {
	w, err := s.d.Works.GetByID(ctx, id)
	if err != nil {
		if seo.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if w == nil {
		return nil, ErrNotFound
	}
	return w, nil
}

// pagesID — адрес диапазона: одна полоса — /pages/a, несколько — /pages/a-b.
func pagesID(w *models.Work, from, to int) string {
	id := seo.PagePath(w.ID, w.Slug, from)
	if to > from {
		id += fmt.Sprintf("-%d", to)
	}
	return id
}

func (s *service) fetchPages(ctx context.Context, a Address) (*FetchOutput, error) {
	w, err := s.work(ctx, a.WorkID)
	if err != nil {
		return nil, err
	}
	b, err := s.d.Library.PageRange(ctx, a.WorkID, a.From, a.To)
	if err != nil {
		return nil, err
	}
	// Внутренние номера фактического диапазона: за концом тома полос меньше,
	// чем просили.
	from, to := b.Meta.PageFrom-w.PageOffset, b.Meta.PageTo-w.PageOffset

	var h strings.Builder
	fmt.Fprintf(&h, "# %s, с. %s\n", w.Title, printedRange(b.Meta.PageFrom, b.Meta.PageTo))
	if author := strings.TrimSpace(w.Author); author != "" {
		h.WriteString(author + "\n")
	}
	if src := book.SourceLine(b.Meta); src != "" {
		h.WriteString("Источник: " + src + "\n")
	}
	h.WriteString("Адрес: " + s.base + seo.PagePath(w.ID, w.Slug, from) + "\n")
	seen := map[int64]bool{}
	for _, n := range []int{from, to} {
		ch, err := s.d.Chapters.FindByPage(ctx, w.ID, n)
		if err != nil {
			return nil, err
		}
		if ch == nil || seen[ch.ID] {
			continue
		}
		seen[ch.ID] = true
		fmt.Fprintf(&h, "Глава «%s» (с. %s): %s%s\n", ch.Title,
			printedRange(ch.StartPage+w.PageOffset, ch.EndPage+w.PageOffset),
			s.base, seo.ChapterPath(w.ID, w.Slug, ch.ID, ch.Slug))
	}
	if from > 1 {
		h.WriteString("Предыдущие полосы: " + pagesID(w, max(1, from-maxPages), from-1) + "\n")
	}
	if to >= a.To {
		h.WriteString("Следующие полосы: " + pagesID(w, to+1, to+maxPages) + "\n")
	}
	h.WriteString("\n" + seo.LLMReadingNote + "\n\n---\n\n")

	text, starts := book.MarkdownWithPageStarts(b)
	body := text
	if len(starts) > 0 {
		// Без титула книги: шапка выше говорит то же короче.
		body = text[starts[0].Offset:]
	}
	return &FetchOutput{
		ID:    pagesID(w, from, to),
		Title: fmt.Sprintf("%s, с. %s", w.Title, printedRange(b.Meta.PageFrom, b.Meta.PageTo)),
		Text:  h.String() + body,
		URL:   s.base + seo.PagePath(w.ID, w.Slug, from),
	}, nil
}

// printedRange — «4» или «4—6».
func printedRange(from, to int) string {
	if to <= from {
		return fmt.Sprint(from)
	}
	return fmt.Sprintf("%d—%d", from, to)
}
