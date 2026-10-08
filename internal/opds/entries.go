package opds

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Размеры страниц лент. Ленты узлов — по 100: у тома 50 Ленина 654 работы
// верхнего уровня, у самого широкого узла корпуса 754 ребёнка, и лента
// целиком стоила бы читалке на e-ink сотен килобайт.
const (
	nodePageSize   = 100
	recentPageSize = 30
	searchPageSize = 30
)

// builder собирает записи с абсолютными адресами от base.
type builder struct {
	base string // https://lib.example.org, без косой в конце
	host string // lib.example.org — для tag: идентификаторов
}

func (b builder) abs(path string) string { return b.base + path }

// tag — постоянный идентификатор. Только число: переименование не делает
// книгу «новой» у агрегатора.
func (b builder) tag(kind string, id int64) string {
	return fmt.Sprintf("tag:%s,2026:%s:%d", b.host, kind, id)
}

func (b builder) feedID(path string) string {
	return "tag:" + b.host + ",2026:opds" + path
}

func (b builder) workDownload(id int64, format string) string {
	return b.abs(fmt.Sprintf("/api/works/%d/download?format=%s", id, format))
}

func (b builder) chapterDownload(workID, chapterID int64, format string) string {
	return b.abs(fmt.Sprintf("/api/works/%d/chapters/%d/download?format=%s", workID, chapterID, format))
}

func (b builder) cover(workID int64) []link {
	href := b.abs(fmt.Sprintf("/og/work/%d.png", workID))
	return []link{
		{Rel: relImage, Href: href, Type: typePNG},
		{Rel: relThumbnail, Href: href, Type: typePNG},
	}
}

// pages — «с. 32—55»; одна полоса — «с. 32»; нет — пусто.
func pages(from, to int) string {
	switch {
	case from <= 0 && to <= 0:
		return ""
	case from == to:
		return "с. " + strconv.Itoa(from)
	default:
		return fmt.Sprintf("с. %d—%d", from, to)
	}
}

func joinSummary(parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func lang(v Volume) string {
	if strings.TrimSpace(v.Lang) == "" {
		return "ru"
	}
	return v.Lang
}

func year(v Volume) string {
	if v.Year <= 0 {
		return ""
	}
	return strconv.Itoa(v.Year)
}

func authors(v Volume) []author {
	if strings.TrimSpace(v.Author) == "" {
		return nil
	}
	return []author{{Name: v.Author}}
}

// volumeSummary — где том стоит: «Сочинения в 13 томах · Том 1 · с. 1—372».
func volumeSummary(v Volume) string {
	return joinSummary(v.EditionTitle, v.VolumeLabel, pages(v.PageFrom, v.PageTo))
}

// volumeBook — том книгой: файл целиком. whole — пометка «— целиком» (в
// ленте самого тома); subsection — ссылка в ленту тома (новые поступления и
// поиск, где у тома нет другого входа в дерево).
func (b builder) volumeBook(v Volume, whole, subsection bool, summary string) entry {
	title := v.Title
	if whole {
		title += " — целиком"
	}
	if summary == "" {
		summary = volumeSummary(v)
	}
	links := []link{
		{Rel: relAcquisition, Href: b.workDownload(v.ID, "epub"), Type: typeEPUB},
		{Rel: relAcquisition, Href: b.workDownload(v.ID, "fb2"), Type: typeFB2},
		{Rel: "alternate", Href: b.abs(v.HTMLPath), Type: typeHTML},
	}
	links = append(links, b.cover(v.ID)...)
	if subsection {
		links = append(links, link{Rel: relSubsection, Href: b.abs(workPath(v.ID)), Type: typeAcquisition})
	}
	return entry{
		ID: b.tag("work", v.ID), Title: title, Updated: stamp(v.Updated),
		Authors: authors(v), Language: lang(v), Issued: year(v),
		Summary: summary, Links: links,
	}
}

// volumeNav — том навигационной записью: вход в его ленту (собрание, тома
// вне собраний). Файлов у записи нет намеренно: у KOReader тап по записи с
// файлами открывает окно скачивания, и до работ внутри тома не дойти.
func (b builder) volumeNav(v Volume) entry {
	links := []link{{Rel: relSubsection, Href: b.abs(workPath(v.ID)), Type: typeAcquisition}}
	links = append(links, b.cover(v.ID)...)
	return entry{
		ID: b.tag("work", v.ID), Title: v.Title, Updated: stamp(v.Updated),
		Authors: authors(v), Summary: volumeSummary(v), Links: links,
	}
}

// nodeBook — глава книгой (лист, либо «Целиком» узла при whole).
func (b builder) nodeBook(v Volume, n *Node, whole bool) entry {
	title := n.Title
	if whole {
		title += " — целиком"
	}
	links := []link{
		{Rel: relAcquisition, Href: b.chapterDownload(v.ID, n.ID, "epub"), Type: typeEPUB},
		{Rel: relAcquisition, Href: b.chapterDownload(v.ID, n.ID, "fb2"), Type: typeFB2},
		{Rel: "alternate", Href: b.abs(n.HTMLPath), Type: typeHTML},
	}
	links = append(links, b.cover(v.ID)...)
	return entry{
		ID: b.tag("chapter", n.ID), Title: title, Updated: stamp(n.Updated),
		Authors: authors(v), Language: lang(v), Issued: year(v),
		Summary: joinSummary(v.EditionTitle, v.VolumeLabel, pages(n.PageFrom, n.PageTo)),
		Links:   links,
	}
}

// nodeNav — узел с детьми: вход в его ленту.
func (b builder) nodeNav(v Volume, n *Node) entry {
	return entry{
		ID: b.tag("chapter", n.ID), Title: n.Title, Updated: stamp(n.Updated),
		Authors: authors(v),
		Summary: joinSummary(fmt.Sprintf("Глав: %d", len(n.Children)), pages(n.PageFrom, n.PageTo)),
		Links: []link{
			{Rel: relSubsection, Href: b.abs(chapterPath(v.ID, n.ID)), Type: typeAcquisition},
		},
	}
}

// node — правило каталога одно на всех уровнях: у узла с детьми —
// навигация, у листа — файлы.
func (b builder) node(v Volume, n *Node) entry {
	if len(n.Children) > 0 {
		return b.nodeNav(v, n)
	}
	return b.nodeBook(v, n, false)
}

func latest(ts ...time.Time) time.Time {
	var out time.Time
	for _, t := range ts {
		if t.After(out) {
			out = t
		}
	}
	return out
}

func volumesLatest(vs []Volume) time.Time {
	var out time.Time
	for _, v := range vs {
		out = latest(out, v.Updated)
	}
	return out
}

// plural — форма слова при числе: 1 полоса, 2 полосы, 5 полос, 11 полос.
func plural(n int, one, few, many string) string {
	n %= 100
	if n < 0 {
		n = -n
	}
	switch {
	case n >= 11 && n <= 14:
		return many
	case n%10 == 1:
		return one
	case n%10 >= 2 && n%10 <= 4:
		return few
	}
	return many
}
