package api

import (
	"net/http"
	"strings"
	"testing"

	"proofreader/internal/models"
)

// M9. Заглавие разбора на сервере не проверялось ни на пустоту, ни на длину:
// пустое проходило, а 501-й знак ронял вставку (колонка documents.title —
// varchar(500), Postgres считает её в ЗНАКАХ) и давал автору 500 вместо
// внятного отказа. Клиент пустое не пускает, но он не единственный вход.
func TestDocumentTitleIsValidatedOnTheServer(t *testing.T) {
	longTitle := strings.Repeat("я", documentTitleMaxRunes+1)
	atLimit := strings.Repeat("я", documentTitleMaxRunes)

	t.Run("создание", func(t *testing.T) {
		store := &fakeDocumentStore{}
		h := NewDocumentHandler(store, nil, &fakeDocumentCutStore{}, &fakeWorkStore{}, nil)

		for _, bad := range []struct{ name, title string }{
			{"пустое", ""},
			{"одни пробелы", "   \n\t "},
			{"длиннее потолка", longTitle},
		} {
			rr := doPostVars(t, h.Create, "/api/documents",
				`{"title":`+jsonString(bad.title)+`,"markdown_content":"тело"}`,
				nil, readerClaims(5, "чтец"))
			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s заглавие прошло с кодом %d: %s", bad.name, rr.Code, rr.Body.String())
			}
		}
		if len(store.docs) != 0 {
			t.Fatalf("негодное заглавие всё-таки завело разбор: %d записей", len(store.docs))
		}

		// Ровно на потолке — проходит, и пробелы по краям срезаны.
		rr := doPostVars(t, h.Create, "/api/documents",
			`{"title":`+jsonString("  "+atLimit+"  ")+`,"markdown_content":"тело"}`,
			nil, readerClaims(5, "чтец"))
		if rr.Code != http.StatusCreated {
			t.Fatalf("заглавие ровно на потолке отвергнуто: код %d (%s)", rr.Code, rr.Body.String())
		}
		if got := store.docs[1].Title; got != atLimit {
			t.Errorf("заглавие записано с обрамляющими пробелами: %q", got)
		}
	})

	t.Run("правка", func(t *testing.T) {
		doc := &models.Document{ID: 7, OwnerID: ptrInt64(5), AuthorNickname: "чтец", Slug: "razbor",
			Title: "Прежнее заглавие", MarkdownContent: "тело",
			ReviewStatus: models.DocumentDraft}
		h := newTestDocumentHandler(t, doc)

		for _, bad := range []struct{ name, title string }{
			{"пустое", ""},
			{"длиннее потолка", longTitle},
		} {
			rr := doPutVars(t, h.Update, "/api/documents/чтец/razbor",
				`{"title":`+jsonString(bad.title)+`,"markdown_content":"новое тело"}`,
				map[string]string{"nickname": "чтец", "slug": "razbor"}, readerClaims(5, "чтец"))
			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s заглавие прошло с кодом %d: %s", bad.name, rr.Code, rr.Body.String())
			}
		}
		// Отказ обязан случиться ДО присвоения полей: негодная правка не
		// вправе тронуть разбор даже в памяти обработчика.
		if doc.Title != "Прежнее заглавие" || doc.MarkdownContent != "тело" {
			t.Fatalf("отвергнутая правка тронула разбор: %q / %q", doc.Title, doc.MarkdownContent)
		}
	})
}

// jsonString — строка как литерал JSON, без ручного экранирования в тесте.
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
