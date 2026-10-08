package seo

import (
	"context"
	"fmt"
	"strconv"

	"proofreader/internal/models"
)

// Построители путей читальни. Номер — ключ, слаг — хвост: пустой слаг даёт
// голый номер, и такой адрес по-прежнему канонический (у главы-нумератора
// слага не бывает вовсе).
func idSegment(id int64, slug string) string {
	if slug == "" {
		return strconv.FormatInt(id, 10)
	}
	return strconv.FormatInt(id, 10) + "-" + slug
}

func workPath(id int64, slug string) string { return "/works/" + idSegment(id, slug) }

func editionPath(id int64, slug string) string { return "/editions/" + idSegment(id, slug) }

func chapterPath(workID int64, workSlug string, chapterID int64, chapterSlug string) string {
	return workPath(workID, workSlug) + "/chapters/" + idSegment(chapterID, chapterSlug)
}

func pagePath(workID int64, workSlug string, page int) string {
	return fmt.Sprintf("%s/pages/%d", workPath(workID, workSlug), page)
}

// WorkPath, ChapterPath и PagePath — построители путей для соседних пакетов
// (MCP-сервер читальни, internal/mcp): адреса в его выдаче обязаны совпадать
// с каноном /seo буква в букву.
func WorkPath(id int64, slug string) string { return workPath(id, slug) }

func ChapterPath(workID int64, workSlug string, chapterID int64, chapterSlug string) string {
	return chapterPath(workID, workSlug, chapterID, chapterSlug)
}

func PagePath(workID int64, workSlug string, page int) string {
	return pagePath(workID, workSlug, page)
}

// documentPath — адрес разбора. Пустая подпись даёт короткий, сотруднический
// вид (/documents/{слаг}), непустая — читательский (/documents/{ник}/{слаг}):
// слаг у разбора уникален только в паре с подписью, и вид адреса выбирает
// сама эта пара, а не число сегментов запроса. Слаг лепит сервер из заглавия
// при создании и больше не меняет, поэтому неканонического вида у разбора не
// существует вовсе — и сверки канона (route.canonical) у него нет.
func documentPath(nickname, slug string) string {
	if nickname == "" {
		return "/documents/" + slug
	}
	return "/documents/" + nickname + "/" + slug
}

// documentCardPath — адрес карточки превью того же разбора. Числовых ключей
// тут нет: у разбора нет второго, числового адреса, как у тома или главы.
func documentCardPath(nickname, slug string) string {
	if nickname == "" {
		return "/og/document/" + slug + ".png"
	}
	return "/og/document/" + nickname + "/" + slug + ".png"
}

// effectiveEditionSlug — адресный слаг издания: editions.url_slug, а при его
// отсутствии — editions.slug (ключ журнала публикации, для адреса тоже
// годится, хоть и менее красив). Пустого url_slug у издания корпуса не
// бывает, но правило одно и не должно расходиться между вызывающими —
// раньше повторялось в render_volume.go (Edition) и render_index.go (Home)
// двумя независимыми копиями.
func effectiveEditionSlug(ed *models.Edition) string {
	if ed.URLSlug != "" {
		return ed.URLSlug
	}
	return ed.Slug
}

// canonicalWork и canonicalChapter добывают канон, не собирая тело: сверка
// канона обязана стоять до занятия слота тяжёлого рендера (см. route.canonical
// в handler.go), и тела полос она поэтому не касается вовсе — только строка
// работы (и, для главы, строка самой главы).
//
// Отсутствие сущности приходит сюда в двух видах, и оба обязаны стать
// ErrNotFound. Голая fmt.Errorf("work not found") — то, что отдаёт сырой
// репозиторий на pgx.ErrNoRows: ErrNotFound в её цепочке нет, а
// writeRenderError различает ошибки только через errors.Is, поэтому
// возвращённая как есть она уезжала в ветку 500. Для поисковика это
// «читальня сломалась, вернусь позже»: адрес остаётся в очереди, а частые
// 500 роняют темп обхода — тогда как 404 адрес просто выбрасывает. Разбор
// вида ошибки — дело isNotFound, единственного места, которое знает про оба.
func canonicalWork(ctx context.Context, s *Source, id int64) (string, error) {
	w, err := s.Works.GetByID(ctx, id)
	if isNotFound(err) {
		return "", notFound("work %d", id)
	}
	if err != nil {
		return "", err
	}
	if w == nil {
		return "", notFound("work %d", id)
	}
	return workPath(w.ID, w.Slug), nil
}

func canonicalChapter(ctx context.Context, s *Source, workID, chapterID int64) (string, error) {
	ch, err := s.Chapters.GetByID(ctx, chapterID)
	if isNotFound(err) {
		return "", notFound("chapter %d of work %d", chapterID, workID)
	}
	if err != nil {
		return "", err
	}
	// ch.WorkID != workID обязателен: номер работы в пути API не сверяется
	// (/works/142/pages/233 отдаёт полосу работы 1, известный дефект
	// проекта), и повторять это в адресах, которые печатает поисковик,
	// нельзя.
	if ch == nil || ch.WorkID != workID {
		return "", notFound("chapter %d of work %d", chapterID, workID)
	}
	w, err := s.Works.GetByID(ctx, workID)
	if isNotFound(err) {
		return "", notFound("work %d", workID)
	}
	if err != nil {
		return "", err
	}
	if w == nil {
		return "", notFound("work %d", workID)
	}
	return chapterPath(w.ID, w.Slug, ch.ID, ch.Slug), nil
}

// canonicalEdition — та же лёгкая сверка для собрания: одно чтение по
// первичному ключу, слаг откатывается effectiveEditionSlug так же, как в
// Edition() (render_volume.go).
func canonicalEdition(ctx context.Context, s *Source, id int64) (string, error) {
	ed, err := s.Editions.GetByID(ctx, id)
	if isNotFound(err) {
		return "", notFound("edition %d", id)
	}
	if err != nil {
		return "", err
	}
	if ed == nil {
		return "", notFound("edition %d", id)
	}
	return editionPath(ed.ID, effectiveEditionSlug(ed)), nil
}
