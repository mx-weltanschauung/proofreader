package seo

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"proofreader/internal/repository"
)

// sitemapChunk — сколько адресов кладётся в один файл карты. Предел
// протокола — 50 000 адресов и 50 МБ; берём с запасом, чтобы кусок оставался
// небольшим и на медленном канале.
const sitemapChunk = 20000

// sitemapKinds — виды карты, которые мы отдаём. Сверка с набором нужна здесь,
// а не в sitemapEntries: там неизвестный вид становится ошибкой, а ошибка
// печатается как 500. Мусорный адрес от бота (/sitemap-chapters-abc.xml) —
// это 404, и путать его с отказом каталога нельзя: по доле 5xx на этом
// обработчике смотрят, жива ли база.
var sitemapKinds = map[string]bool{
	"works":       true,
	"chapters":    true,
	"editions":    true,
	"concepts":    true,
	"collections": true,
	"documents":   true,
}

// isPublicCollectionRow — сотрудническая (не читательская) и опубликованная
// подборка. Читательские подборки не модерируются: без отбора спам от
// читателя оказался бы прямо в витрине и в карте сайта, а ссылка на
// неопубликованный черновик — вовсе не должна существовать снаружи. Отбор
// зеркалит SQL-фильтр CollectionRepository.List (пустой author_nickname и
// непустой published_at), но сделан здесь Go-кодом поверх CatalogRow, а не
// в запросе: подставной каталог тестов пакета seo (fakeCatalog) не исполняет
// SQL, и фильтр в запросе был бы невидим тесту на мутацию.
func isPublicCollectionRow(row repository.CatalogRow) bool {
	return row.AuthorNickname == "" && row.PublishedAt != nil
}

// publicCollectionRows — единая точка отбора для карты сайта (sitemap.go) и
// общей карточки (card.go, siteCard): один фильтр на оба места, чтобы список
// подборок и их счёт на карточке не расходились.
func publicCollectionRows(rows []repository.CatalogRow) []repository.CatalogRow {
	out := make([]repository.CatalogRow, 0, len(rows))
	for _, row := range rows {
		if isPublicCollectionRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// isPublicDocumentRow — опубликованный разбор, ЛЮБОГО вида: читательский
// наравне с сотрудническим.
//
// Разбор ЗДЕСЬ намеренно расходится с подборкой (isPublicCollectionRow выше
// отсеивает читательские по author_nickname): подборку никто не модерирует,
// а читательский разбор проходит одобрение редактора точно так же, как
// сотруднический, и к моменту публикации уже просмотрен — прятать его из
// витрины и карты сайта по одному лишь признаку авторства значило бы удвоить
// проверку, которая уже прошла. Решение владельца 19.09.2026: не «чинить» по
// образцу подборки, если это когда-нибудь покажется несимметричным — разница
// не забытая мелочь, а точка, в которой два вида контента различаются по
// смыслу (модерация против её отсутствия).
func isPublicDocumentRow(row repository.CatalogRow) bool {
	return row.PublishedAt != nil
}

// publicDocumentRows — единая точка отбора для карты сайта и общей карточки
// (card.go, siteCard), тем же приёмом, что и publicCollectionRows.
func publicDocumentRows(rows []repository.CatalogRow) []repository.CatalogRow {
	out := make([]repository.CatalogRow, 0, len(rows))
	for _, row := range rows {
		if isPublicDocumentRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// urlSet и urlEntry печатаются через encoding/xml: экранирование амперсандов и
// угловых скобок в адресах достаётся бесплатно, а собранная строками карта на
// первом же слаге с амперсандом стала бы неразбираемой.
type urlSet struct {
	XMLName xml.Name   `xml:"urlset"`
	NS      string     `xml:"xmlns,attr"`
	URLs    []urlEntry `xml:"url"`
}

type urlEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemapIndex struct {
	XMLName xml.Name       `xml:"sitemapindex"`
	NS      string         `xml:"xmlns,attr"`
	Maps    []sitemapEntry `xml:"sitemap"`
}

type sitemapEntry struct {
	Loc string `xml:"loc"`
}

const sitemapNS = "http://www.sitemaps.org/schemas/sitemap/0.9"

// Sitemap отдаёт один кусок карты: /sitemap-works.xml,
// /sitemap-chapters-3.xml и так далее.
func (h *Handler) Sitemap(w http.ResponseWriter, r *http.Request) {
	kind, chunk, ok := parseSitemapPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	entries, err := h.sitemapEntries(r.Context(), kind)
	if err != nil {
		log.Printf("карта сайта %s: %v", kind, err)
		http.Error(w, "Не удалось собрать карту сайта", http.StatusInternalServerError)
		return
	}

	// У глав куски нумерованные, у остальных — единственный файл без номера.
	if kind == "chapters" {
		from := (chunk - 1) * sitemapChunk
		if from >= len(entries) && !(from == 0 && len(entries) == 0) {
			http.NotFound(w, r)
			return
		}
		to := from + sitemapChunk
		if to > len(entries) {
			to = len(entries)
		}
		entries = entries[from:to]
	}

	writeXML(w, urlSet{NS: sitemapNS, URLs: entries})
}

// SitemapIndex перечисляет все куски. Кусок, на который никто не ссылается,
// поисковик не найдёт, поэтому число кусков глав считается, а не угадывается.
func (h *Handler) SitemapIndex(w http.ResponseWriter, r *http.Request) {
	chapters, err := h.src.Catalog.Chapters(r.Context())
	if err != nil {
		log.Printf("карта сайта, индекс: %v", err)
		http.Error(w, "Не удалось собрать карту сайта", http.StatusInternalServerError)
		return
	}

	idx := sitemapIndex{NS: sitemapNS}
	for _, kind := range []string{"works", "editions", "concepts", "collections", "documents"} {
		idx.Maps = append(idx.Maps, sitemapEntry{Loc: h.src.abs("/sitemap-" + kind + ".xml")})
	}
	chunks := (len(chapters) + sitemapChunk - 1) / sitemapChunk
	if chunks == 0 {
		chunks = 1
	}
	for i := 1; i <= chunks; i++ {
		idx.Maps = append(idx.Maps,
			sitemapEntry{Loc: h.src.abs(fmt.Sprintf("/sitemap-chapters-%d.xml", i))})
	}

	writeXML(w, idx)
}

func (h *Handler) sitemapEntries(ctx context.Context, kind string) ([]urlEntry, error) {
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format("2006-01-02")
	}

	switch kind {
	case "works":
		rows, err := h.src.Catalog.Works(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]urlEntry, 0, len(rows))
		for _, row := range rows {
			out = append(out, urlEntry{
				Loc:     h.src.abs(workPath(row.ID, row.Slug)),
				LastMod: stamp(row.UpdatedAt),
			})
		}
		return out, nil

	case "chapters":
		rows, err := h.src.Catalog.Chapters(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]urlEntry, 0, len(rows))
		for _, row := range rows {
			out = append(out, urlEntry{
				Loc:     h.src.abs(chapterPath(row.WorkID, row.WorkSlug, row.ID, row.Slug)),
				LastMod: stamp(row.UpdatedAt),
			})
		}
		return out, nil

	case "editions":
		rows, err := h.src.Catalog.Editions(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]urlEntry, 0, len(rows))
		for _, row := range rows {
			out = append(out, urlEntry{
				Loc:     h.src.abs(editionPath(row.ID, row.Slug)),
				LastMod: stamp(row.UpdatedAt),
			})
		}
		return out, nil

	case "concepts":
		rows, err := h.src.Catalog.Concepts(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]urlEntry, 0, len(rows))
		for _, row := range rows {
			out = append(out, urlEntry{
				Loc:     h.src.abs("/concepts/" + row.Slug),
				LastMod: stamp(row.UpdatedAt),
			})
		}
		return out, nil

	case "collections":
		rows, err := h.src.Catalog.Collections(ctx)
		if err != nil {
			return nil, err
		}
		// Читательские подборки без модерации: без отбора спам от читателя
		// попал бы прямо в карту сайта. Тот же отбор — publicCollectionRows —
		// зовёт и общая карточка (card.go, siteCard), чтобы список и карта не
		// разъехались в счёте.
		rows = publicCollectionRows(rows)
		out := make([]urlEntry, 0, len(rows))
		for _, row := range rows {
			out = append(out, urlEntry{
				Loc:     h.src.abs("/collections/" + row.Slug),
				LastMod: stamp(row.UpdatedAt),
			})
		}
		return out, nil

	case "documents":
		rows, err := h.src.Catalog.Documents(ctx)
		if err != nil {
			return nil, err
		}
		// publicDocumentRows, а НЕ publicCollectionRows: разбор проходит
		// модерацию редактора независимо от того, кто его написал, и в карту
		// сайта идут ОБЕ разновидности опубликованного разбора — читательская
		// наравне с сотруднической (см. isPublicDocumentRow, выше). Тот же
		// отбор зовёт и карточка витрины (card.go, siteCard), чтобы счёт и
		// состав карты не разошлись.
		rows = publicDocumentRows(rows)
		out := make([]urlEntry, 0, len(rows))
		for _, row := range rows {
			out = append(out, urlEntry{
				Loc:     h.src.abs(documentPath(row.AuthorNickname, row.Slug)),
				LastMod: stamp(row.UpdatedAt),
			})
		}
		return out, nil
	}

	return nil, fmt.Errorf("неизвестный вид карты %q", kind)
}

// parseSitemapPath разбирает /sitemap-<вид>[-<номер>].xml. Номер есть только у
// карты глав; у остальных видов кусок один и адрес без номера.
func parseSitemapPath(path string) (kind string, chunk int, ok bool) {
	if !strings.HasSuffix(path, ".xml") {
		return "", 0, false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, "/"), ".xml")
	if !strings.HasPrefix(name, "sitemap-") {
		return "", 0, false
	}
	rest := strings.TrimPrefix(name, "sitemap-")
	if rest == "" {
		return "", 0, false
	}

	// Проверим, есть ли в адресе номер после последнего дефиса.
	if i := strings.LastIndex(rest, "-"); i > 0 {
		if n, err := strconv.Atoi(rest[i+1:]); err == nil && n >= 1 {
			// Номер есть. Он осмыслен только для глав.
			candidate := rest[:i]
			if !sitemapKinds[candidate] {
				return "", 0, false
			}
			if candidate != "chapters" {
				// Остальные виды не нумеруются.
				return "", 0, false
			}
			return candidate, n, true
		}
	}

	// Номера нет. Это должен быть один из известных видов.
	if !sitemapKinds[rest] {
		return "", 0, false
	}
	return rest, 1, true
}

func writeXML(w http.ResponseWriter, doc any) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		return
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		log.Printf("карта сайта: %v", err)
	}
}
