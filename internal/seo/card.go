package seo

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"net/http"
	"proofreader/internal/site"
	"strconv"
	"strings"
	"sync"
	"time"

	"proofreader/internal/repository"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Шрифт вшит в бинарник: карточка рисуется на каждом первом запросе превью, и
// зависеть от файла на диске боевого ей незачем. Начертания подрезаны до
// кириллицы с латиницей (см. шаг 1 задачи 9).
//
//go:embed assets/Literata-Regular.ttf assets/Literata-Bold.ttf
var fontFS embed.FS

const (
	// Размер карточки — то же, что уходит в og:image:width/height. Держать
	// его константой обязательно: разойдись он с тегами, Телеграм обрежет
	// картинку по заявленным пропорциям.
	cardWidth  = 1200
	cardHeight = 630
	cardPad    = 80
	// cardTitleLines — сколько строк отводится заголовку. Больше четырёх на
	// 1200×630 читаются мелко, поэтому кегль подбирается под этот предел.
	cardTitleLines = 4
	// subtitleFootGap — минимальный просвет между подзаголовком и подвалом.
	// Заголовок в четыре строки поднимает подзаголовок почти к самому низу
	// карточки; без этого зазора он наезжал бы на подвал (см. вызов в Card).
	subtitleFootGap = 24
)

var (
	cardPaper = color.RGBA{0xfd, 0xfd, 0xfb, 0xff}
	cardInk   = color.RGBA{0x1a, 0x1a, 0x1a, 0xff}
	cardMuted = color.RGBA{0x8a, 0x85, 0x78, 0xff}
	// cardSpine — тёмная полоса слева, как корешок книги. Первый вариант,
	// его судят глазами через TestCardDump.
	cardSpine = color.RGBA{0x2f, 0x2b, 0x24, 0xff}
)

// CardInput — всё, что попадает на карточку. Заголовок обязателен, остальное
// печатается, если непусто.
type CardInput struct {
	Title    string
	Subtitle string // автор, рубрика, «Подборка»
	Footer   string // издание с томом, число адресов
}

var (
	fontOnce    sync.Once
	fontRegular *sfnt.Font
	fontBold    *sfnt.Font
	fontErr     error
)

func loadFonts() error {
	fontOnce.Do(func() {
		reg, err := fontFS.ReadFile("assets/Literata-Regular.ttf")
		if err != nil {
			fontErr = fmt.Errorf("шрифт Regular: %w", err)
			return
		}
		bold, err := fontFS.ReadFile("assets/Literata-Bold.ttf")
		if err != nil {
			fontErr = fmt.Errorf("шрифт Bold: %w", err)
			return
		}
		if fontRegular, fontErr = opentype.Parse(reg); fontErr != nil {
			return
		}
		fontBold, fontErr = opentype.Parse(bold)
	})
	return fontErr
}

func newFace(f *sfnt.Font, size float64) (font.Face, error) {
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
}

func Card(in CardInput) ([]byte, error) {
	if err := loadFonts(); err != nil {
		return nil, err
	}

	img := image.NewRGBA(image.Rect(0, 0, cardWidth, cardHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{cardPaper}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, 16, cardHeight), &image.Uniform{cardSpine}, image.Point{}, draw.Src)

	maxTextWidth := cardWidth - 2*cardPad

	// Подвал прижат к низу, а не к тексту: у карточек с коротким и длинным
	// заголовком он должен стоять на одной высоте, иначе в ленте они выглядят
	// разными шаблонами. Считаем его позицию заранее — она нужна и подзаголовку,
	// чтобы не наехать на подвал при заголовке в четыре строки (см. ниже).
	footFace, err := newFace(fontRegular, 28)
	if err != nil {
		return nil, err
	}
	footY := cardHeight - cardPad/2 - footFace.Metrics().Descent.Ceil()

	// Кегль заголовка подбирается вниз, пока строки не уместятся в отведённые
	// четыре. Если не уместились и на самом мелком — обрезаем с отбивкой:
	// уехавший за край текст хуже сокращённого.
	titleFace, titleLines, err := fitTitle(in.Title, maxTextWidth)
	if err != nil {
		return nil, err
	}

	y := cardPad + titleFace.Metrics().Ascent.Ceil()
	titleLineHeight := titleFace.Metrics().Height.Ceil() * 115 / 100
	drawer := &font.Drawer{Dst: img, Src: &image.Uniform{cardInk}, Face: titleFace}
	for _, line := range titleLines {
		drawer.Dot = fixed.P(cardPad, y)
		drawer.DrawString(line)
		y += titleLineHeight
	}

	if in.Subtitle != "" {
		face, err := newFace(fontRegular, 34)
		if err != nil {
			return nil, err
		}
		y += face.Metrics().Ascent.Ceil()
		// Заголовок в четыре строки почти достаёт до подвала: без потолка
		// подзаголовок печатался бы поверх «Тома N» — оба у одного левого
		// края, и на карточке они сливались бы в нечитаемую кашу (видно на
		// TestCardDump/chapter-long.png без этой отбивки). subtitleFootGap —
		// просвет, который отделяет их даже в этом крайнем случае.
		if maxY := footY - face.Metrics().Descent.Ceil() - subtitleFootGap; y > maxY {
			y = maxY
		}
		d := &font.Drawer{Dst: img, Src: &image.Uniform{cardMuted}, Face: face}
		d.Dot = fixed.P(cardPad, y)
		d.DrawString(truncateToWidth(face, OneLine(in.Subtitle), maxTextWidth))
	}

	if in.Footer != "" {
		d := &font.Drawer{Dst: img, Src: &image.Uniform{cardMuted}, Face: footFace}
		d.Dot = fixed.P(cardPad, footY)
		d.DrawString(truncateToWidth(footFace, OneLine(in.Footer), maxTextWidth*2/3))
	}

	nameFace, err := newFace(fontBold, 28)
	if err != nil {
		return nil, err
	}
	nameWidth := font.MeasureString(nameFace, site.Name()).Ceil()
	d := &font.Drawer{Dst: img, Src: &image.Uniform{cardSpine}, Face: nameFace}
	d.Dot = fixed.P(cardWidth-cardPad-nameWidth, footY)
	d.DrawString(site.Name())

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("кодирование карточки: %w", err)
	}
	return buf.Bytes(), nil
}

func fitTitle(title string, maxWidth int) (font.Face, []string, error) {
	sizes := []float64{64, 56, 48, 42, 36}
	var face font.Face
	var lines []string

	for i, size := range sizes {
		f, err := newFace(fontBold, size)
		if err != nil {
			return nil, nil, err
		}
		l := wrapWords(f, OneLine(title), maxWidth)
		face, lines = f, l
		if len(l) <= cardTitleLines || i == len(sizes)-1 {
			break
		}
	}

	if len(lines) > cardTitleLines {
		lines = lines[:cardTitleLines]
		lines[cardTitleLines-1] = strings.TrimRight(lines[cardTitleLines-1], " ,;:—-") + "…"
	}
	return face, lines, nil
}

// wrapWords переносит по словам. Слово, не влезающее в строку целиком,
// остаётся в своей строке как есть: разрывать его посередине хуже, чем
// дать ему торчать, а на подрезке кегля оно всё равно поместится.
func wrapWords(f font.Face, text string, maxWidth int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	cur := words[0]
	for _, w := range words[1:] {
		try := cur + " " + w
		if font.MeasureString(f, try).Ceil() <= maxWidth {
			cur = try
			continue
		}
		lines = append(lines, cur)
		cur = w
	}
	return append(lines, cur)
}

func truncateToWidth(f font.Face, text string, maxWidth int) string {
	if font.MeasureString(f, text).Ceil() <= maxWidth {
		return text
	}
	runes := []rune(text)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if font.MeasureString(f, candidate).Ceil() <= maxWidth {
			return candidate
		}
	}
	return "…"
}

// OGCard отдаёт карточку: /og/{вид}/{адрес}.png. Без префикса /api намеренно
// — ссылка на неё живёт в чужих кэшах годами и должна выглядеть как ссылка на
// картинку.
func (h *Handler) OGCard(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := parseCardPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	cacheKey := "/og/" + kind + "/" + id
	// Метка поколения — до рисования, по той же причине, что у страниц
	// (см. Handler.Page): у карточек срок годности сутки, и карточка снятого
	// тома, легшая в кэш после сброса, пережила бы снятие на сутки.
	gen := h.cards.generation()
	if cached, ok := h.cards.get(cacheKey); ok {
		writeCard(w, r, cached)
		return
	}

	in, stamp, err := h.src.cardInput(r.Context(), kind, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("карточка %s: %v", cacheKey, err)
		http.Error(w, "Не удалось нарисовать карточку", http.StatusInternalServerError)
		return
	}

	raw, err := Card(in)
	if err != nil {
		log.Printf("карточка %s: %v", cacheKey, err)
		http.Error(w, "Не удалось нарисовать карточку", http.StatusInternalServerError)
		return
	}

	entry := &cacheEntry{body: string(raw)}
	if !stamp.IsZero() {
		entry.etag = docETag(cacheKey, h.build(), stamp.Unix())
	}
	h.cards.putIfFresh(cacheKey, entry, gen)
	writeCard(w, r, entry)
}

func writeCard(w http.ResponseWriter, r *http.Request, e *cacheEntry) {
	if e.etag != "" {
		w.Header().Set("ETag", e.etag)
		if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, e.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("Content-Type", "image/png")
	// Сутки: Телеграм всё равно забирает карточку один раз и держит её у себя
	// сам, а поисковики перезаходят по ETag.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if _, err := w.Write([]byte(e.body)); err != nil {
		log.Printf("карточка: отдача прервана: %v", err)
	}
}

// fourSegmentCardKinds — виды карточек, чей слаг уникален только в паре с
// ником автора (читательская подборка, читательский разбор), а не сам по
// себе — им нужен четвёртый сегмент адреса. Множество, а не перечисленное
// условие: третий такой вид добавляется одной строкой сюда, без правки
// parseCardPath.
var fourSegmentCardKinds = map[string]bool{
	"collection": true,
	"document":   true,
}

// parseCardPath разбирает /og/{вид}/{адрес}.png. Адрес — число у тома,
// издания и главы, слаг у понятия и сотруднической подборки/разбора; у
// читательской подборки и читательского разбора слаг уникален только в паре
// с ником автора (второй, более длинный адрес), поэтому для видов из
// fourSegmentCardKinds допускается и четыре сегмента —
// /og/collection/{ник}/{слаг}.png, /og/document/{ник}/{слаг}.png. id тогда
// возвращается как "ник/слаг" и разбирается дальше в cardInput.
func parseCardPath(path string) (kind, id string, ok bool) {
	if !strings.HasSuffix(path, ".png") {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(path, ".png"), "/"), "/")
	if len(parts) == 4 && parts[0] == "og" && fourSegmentCardKinds[parts[1]] && parts[2] != "" && parts[3] != "" {
		return parts[1], parts[2] + "/" + parts[3], true
	}
	if len(parts) != 3 || parts[0] != "og" || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// splitAuthorID разбирает id карточки подборки или разбора обратно на ник и
// слаг: "слаг" (сотрудническая/сотруднический) или "ник/слаг" (читательская
// подборка, читательский разбор — см. parseCardPath и fourSegmentCardKinds).
func splitAuthorID(id string) (nickname, slugPart string) {
	if i := strings.IndexByte(id, '/'); i >= 0 {
		return id[:i], id[i+1:]
	}
	return "", id
}

// cardInput добывает надписи карточки. Метка времени идёт в ETag: том
// переименовали — карточка обновится у поисковика, хотя у Телеграма останется
// старая (он превью не перезапрашивает).
func (s *Source) cardInput(ctx context.Context, kind, id string) (CardInput, time.Time, error) {
	numeric := func() (int64, error) { return strconv.ParseInt(id, 10, 64) }

	switch kind {
	case "work":
		workID, err := numeric()
		if err != nil {
			return CardInput{}, time.Time{}, notFound("карточка work %q", id)
		}
		work, err := s.Works.GetByID(ctx, workID)
		if err != nil {
			if isNotFound(err) {
				return CardInput{}, time.Time{}, notFound("работа %d", workID)
			}
			return CardInput{}, time.Time{}, fmt.Errorf("работа %d: %w", workID, err)
		}
		if work == nil {
			return CardInput{}, time.Time{}, notFound("работа %d", workID)
		}
		edition := ""
		if work.EditionID != nil {
			// Тот же разбор, что в render_volume.go: отсутствие издания
			// молчаливо оставляет карточку без строки издания, а вот отказ
			// базы молчанием прикрывать нельзя — иначе больная база тихо
			// отдаёт краулеру и Телеграму урезанную карточку.
			ed, err := s.Editions.GetByID(ctx, *work.EditionID)
			switch {
			case isNotFound(err):
			case err != nil:
				log.Printf("карточка work %d: не удалось получить издание %d: %v",
					workID, *work.EditionID, err)
			case ed != nil:
				edition = ed.Title
			}
		}
		return CardInput{Title: work.Title, Subtitle: work.Author, Footer: edition},
			work.UpdatedAt, nil

	case "chapter":
		chapterID, err := numeric()
		if err != nil {
			return CardInput{}, time.Time{}, notFound("карточка chapter %q", id)
		}
		chapter, err := s.Chapters.GetByID(ctx, chapterID)
		if err != nil {
			if isNotFound(err) {
				return CardInput{}, time.Time{}, notFound("глава %d", chapterID)
			}
			return CardInput{}, time.Time{}, fmt.Errorf("глава %d: %w", chapterID, err)
		}
		if chapter == nil {
			return CardInput{}, time.Time{}, notFound("глава %d", chapterID)
		}
		author, footer := "", ""
		// Та же осторожность, что у издания в кейсе "work" выше: не найдено —
		// молчим, отказ базы — логируем, не проглатываем.
		work, err := s.Works.GetByID(ctx, chapter.WorkID)
		switch {
		case isNotFound(err):
		case err != nil:
			log.Printf("карточка chapter %d: не удалось получить работу %d: %v",
				chapterID, chapter.WorkID, err)
		case work != nil:
			author = work.Author
			footer = work.Title
		}
		return CardInput{Title: chapter.Title, Subtitle: author, Footer: footer},
			chapter.UpdatedAt, nil

	case "edition":
		editionID, err := numeric()
		if err != nil {
			return CardInput{}, time.Time{}, notFound("карточка edition %q", id)
		}
		ed, err := s.Editions.GetByID(ctx, editionID)
		if err != nil {
			if isNotFound(err) {
				return CardInput{}, time.Time{}, notFound("издание %d", editionID)
			}
			return CardInput{}, time.Time{}, fmt.Errorf("издание %d: %w", editionID, err)
		}
		if ed == nil {
			return CardInput{}, time.Time{}, notFound("издание %d", editionID)
		}
		works, err := s.Editions.ListWorks(ctx, editionID)
		if err != nil {
			// Список пуст в счётчике, но не молча: без строчки в лог отказ
			// базы неотличим от честного «томов ещё нет».
			log.Printf("карточка edition %d: не удалось получить список томов: %v", editionID, err)
		}
		return CardInput{
			Title:    ed.Title,
			Subtitle: "Собрание сочинений",
			Footer:   fmt.Sprintf("Томов в читальне: %d", len(works)),
		}, ed.UpdatedAt, nil

	case "concept":
		c, err := s.Concepts.GetConceptBySlug(ctx, id)
		if err != nil {
			if isNotFound(err) {
				return CardInput{}, time.Time{}, notFound("понятие %q", id)
			}
			return CardInput{}, time.Time{}, fmt.Errorf("понятие %q: %w", id, err)
		}
		if c == nil {
			return CardInput{}, time.Time{}, notFound("понятие %q", id)
		}
		// Счётчик — сумма адресов по всем статьям понятия, а не длина
		// c.References: это поле — временная совместимость, заполненная
		// только первой статьёй (см. render_index.go, Concept).
		addresses := 0
		for _, a := range c.Articles {
			addresses += len(a.References)
		}
		return CardInput{
			Title:    c.Title,
			Subtitle: "Предметный указатель",
			Footer:   fmt.Sprintf("Адресов в корпусе: %d", addresses),
		}, c.UpdatedAt, nil

	case "collection":
		// id — "слаг" у сотруднической подборки или "ник/слаг" у
		// читательской (см. parseCardPath): слаг там уникален только в паре с
		// ником, а не сам по себе.
		nickname, slugPart := splitAuthorID(id)
		c, err := s.Collections.GetByAuthorSlug(ctx, nickname, slugPart)
		if err != nil {
			if isNotFound(err) {
				return CardInput{}, time.Time{}, notFound("подборка %q", id)
			}
			return CardInput{}, time.Time{}, fmt.Errorf("подборка %q: %w", id, err)
		}
		// Черновик снаружи не существует вовсе: ни страницы, ни карточки —
		// иначе ссылка, разосланная до публикации, всё равно дала бы превью.
		if c == nil || c.PublishedAt == nil {
			return CardInput{}, time.Time{}, notFound("подборка %q", id)
		}
		subtitle := "Подборка"
		if c.AuthorNickname != "" {
			// Несъёмная пометка происхождения: читальня отвечает за то, что
			// показывает, и не модерирует читательские подборки — читатель
			// должен быть виден как собиратель, автор карточки его убрать не
			// может.
			subtitle = "Подборка · собрал читатель " + c.AuthorNickname
		}
		return CardInput{
			Title:    c.Title,
			Subtitle: subtitle,
			Footer:   fmt.Sprintf("Пунктов: %d", len(c.Items)),
		}, c.UpdatedAt, nil

	case "document":
		// id — "слаг" у сотруднического разбора или "ник/слаг" у
		// читательского (см. parseCardPath и splitAuthorID), тем же приёмом,
		// что у подборки выше.
		nickname, slugPart := splitAuthorID(id)
		card, err := s.Documents.Card(ctx, nickname, slugPart)
		if err != nil {
			// Карточка не различает 404 (никогда не публиковался) и 410
			// (сняли) — тем же единым отказом, что уже принят у подборки
			// выше: сам OGCard (обработчик) сверяет только ErrNotFound, и
			// у подборки нет отдельной ветки под ErrNeverPublished тоже.
			// «Ни страницы, ни карточки» черновику — общее правило уровня
			// карточек, а не частность разбора.
			if isNotFound(err) || isNeverPublished(err) {
				return CardInput{}, time.Time{}, notFound("разбор %q", id)
			}
			return CardInput{}, time.Time{}, fmt.Errorf("разбор %q: %w", id, err)
		}
		d := card.Document
		subtitle := "Разбор"
		if d.AuthorNickname != "" {
			subtitle = "Разбор · собрал читатель " + d.AuthorNickname
		}
		// Title — d.Title, а не d.PublishedTitle: card.Document уже сведён к
		// одобренной редакции внутри Documents.Card (documentForViewer
		// копирует PublishedTitle в Title до того, как строка сюда попадёт),
		// поэтому здесь она читается напрямую — так же, как и у страницы
		// краулера (render_document.go, Document).
		return CardInput{
			Title:    d.Title,
			Subtitle: subtitle,
			Footer:   fmt.Sprintf("Вклеек: %d", card.CutCount),
		}, d.UpdatedAt, nil

	// Страницы-списки своей строки в базе не имеют, но ссылка на корень
	// читальни пересылается чаще любой другой: без карточки она уходит в
	// мессенджер голым текстом, тогда как ссылка на любой том — с картинкой.
	case "site":
		return s.siteCard(ctx, id)
	}

	return CardInput{}, time.Time{}, notFound("вид карточки %q", kind)
}

// siteCard — надписи карточки для страницы-списка. Счёт берётся из каталога
// (того же, что печатает карту сайта), а не из списков самих страниц: у тех стоит
// потолок (conceptListCap), и карточка начала бы врать, как только указатель
// перерастёт этот потолок. Метка времени — самая свежая из строк: карточка
// несёт их число, и правка любой из них обязана сбрасывать ETag.
func (s *Source) siteCard(ctx context.Context, id string) (CardInput, time.Time, error) {
	count := func(rows []repository.CatalogRow) (int, time.Time) {
		var newest time.Time
		for _, row := range rows {
			if row.UpdatedAt.After(newest) {
				newest = row.UpdatedAt
			}
		}
		return len(rows), newest
	}

	switch id {
	case "home":
		rows, err := s.Catalog.Editions(ctx)
		if err != nil {
			return CardInput{}, time.Time{}, fmt.Errorf("каталог изданий: %w", err)
		}
		n, stamp := count(rows)
		return CardInput{
			Title:    site.Name(),
			Subtitle: site.Description(),
			Footer:   fmt.Sprintf("Собраний сочинений: %d", n),
		}, stamp, nil

	// Подзаголовки здесь описательные, а не site.Name(): марка читальни стоит в
	// правом нижнем углу каждой карточки, и то же слово подзаголовком
	// печаталось бы на картинке дважды.
	case "concepts":
		rows, err := s.Catalog.Concepts(ctx)
		if err != nil {
			return CardInput{}, time.Time{}, fmt.Errorf("каталог понятий: %w", err)
		}
		n, stamp := count(rows)
		return CardInput{
			Title:    "Предметный указатель",
			Subtitle: "Понятия корпуса с адресами в томах",
			Footer:   fmt.Sprintf("Понятий: %d", n),
		}, stamp, nil

	case "collections":
		rows, err := s.Catalog.Collections(ctx)
		if err != nil {
			return CardInput{}, time.Time{}, fmt.Errorf("каталог подборок: %w", err)
		}
		// Тот же отбор, что у карты сайта (sitemap.go): витрина не показывает
		// читательские подборки и черновики, и карточка витрины не должна
		// считать их в числе.
		n, stamp := count(publicCollectionRows(rows))
		return CardInput{
			Title:    "Подборки",
			Subtitle: "Читательские подборки глав и работ",
			Footer:   fmt.Sprintf("Подборок: %d", n),
		}, stamp, nil

	case "documents":
		rows, err := s.Catalog.Documents(ctx)
		if err != nil {
			return CardInput{}, time.Time{}, fmt.Errorf("каталог разборов: %w", err)
		}
		// publicDocumentRows, а НЕ publicCollectionRows: разбор здесь
		// намеренно расходится с подборкой — читательский разбор
		// проходит модерацию наравне с сотрудническим и считается в
		// витрине точно так же (см. комментарий у isPublicDocumentRow,
		// sitemap.go). Не «чинить» по образцу подборки.
		n, stamp := count(publicDocumentRows(rows))
		return CardInput{
			Title:    "Разборы",
			Subtitle: "Разборы читателей и редакции",
			Footer:   fmt.Sprintf("Разборов: %d", n),
		}, stamp, nil
	}

	// Алиасов здесь нет ровно по той же причине, что и у видов карточек:
	// чужой кэш держит картинку годами, и второй адрес у одной карточки
	// означает две её версии, которые никогда не сойдутся.
	return CardInput{}, time.Time{}, notFound("карточка site %q", id)
}
