// Package staticsite собирает статическую читальню: весь текст корпуса
// папкой HTML-файлов, которая открывается и с диска (file://), и с любого
// статического хостинга. Спека:
// docs/superpowers/specs/2026-10-06-static-reading-room-design.md.
package staticsite

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"proofreader/pkg/slug"
)

// Все пути здесь — от корня сборки, без ведущей косой черты, и всегда на
// файл, а не на каталог: через file:// браузер не подставляет index.html.

const (
	HomeFile      = "index.html"
	ConceptsIndex = "concepts/index.html"
	SearchFile    = "search/index.html"
)

func idSegment(id int64, s string) string {
	if s == "" {
		return strconv.FormatInt(id, 10)
	}
	return strconv.FormatInt(id, 10) + "-" + s
}

func workDir(id int64, s string) string { return "works/" + idSegment(id, s) }

func WorkIndex(id int64, s string) string { return workDir(id, s) + "/index.html" }

func ChapterFile(workID int64, workSlug string, chapterID int64, chapterSlug string) string {
	return workDir(workID, workSlug) + "/" + idSegment(chapterID, chapterSlug) + ".html"
}

// GapFile — полосы тома, не накрытые ни одной главой, начиная с полосы
// startPage (внутренний номер). Имя не начинается с цифры, поэтому с файлом
// главы не совпадёт.
func GapFile(workID int64, workSlug string, startPage int) string {
	return fmt.Sprintf("%s/vne-glav-%d.html", workDir(workID, workSlug), startPage)
}

func EditionIndex(s string) string { return "editions/" + slug.Text(s) + "/index.html" }

// ConceptFile — слаг понятия бывает до 252 знаков, а Windows не открывает
// путь длиннее 260. Читаемая часть режется до slug.MaxLen, а различает
// понятия хвост из sha256 полного слага.
func ConceptFile(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "concepts/" + slug.Text(s) + "-" + hex.EncodeToString(sum[:4]) + ".html"
}

func CollectionFile(id int64, s string) string {
	return "collections/" + idSegment(id, slug.Text(s)) + ".html"
}

func DocumentFile(id int64, s string) string {
	return "documents/" + idSegment(id, slug.Text(s)) + ".html"
}

// rel — ссылка из файла from на to (оба от корня; у to может быть #якорь).
// Глубина from — число косых черт в нём: каждая означает один «../».
func rel(from, to string) string {
	target, frag, hasFrag := strings.Cut(to, "#")
	if target == from && hasFrag {
		return "#" + frag
	}
	out := strings.Repeat("../", strings.Count(from, "/")) + target
	if hasFrag {
		out += "#" + frag
	}
	return out
}
