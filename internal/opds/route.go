package opds

import (
	"strconv"
	"strings"
)

// Виды лент. Они же — kind строки посещаемости канала opds
// (api.channelRow), поэтому разбор пути один на обработчик и счёт.
const (
	KindRoot       = "root"
	KindEditions   = "editions"
	KindEdition    = "edition"
	KindLoose      = "loose"
	KindNew        = "new"
	KindWork       = "work"
	KindChapter    = "chapter"
	KindSearch     = "search"
	KindOpenSearch = "opensearch"
)

// Route — разобранный путь под /opds.
type Route struct {
	Kind      string
	EditionID int64
	WorkID    int64
	ChapterID int64
}

// ParsePath разбирает путь запроса (без query). false — пути нет в каталоге.
func ParsePath(path string) (Route, bool) {
	rest, ok := strings.CutPrefix(path, "/opds")
	if !ok || (rest != "" && rest[0] != '/') {
		return Route{}, false
	}
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return Route{Kind: KindRoot}, true
	}
	seg := strings.Split(rest, "/")
	switch {
	case len(seg) == 1 && seg[0] == "editions":
		return Route{Kind: KindEditions}, true
	case len(seg) == 1 && seg[0] == "loose":
		return Route{Kind: KindLoose}, true
	case len(seg) == 1 && seg[0] == "new":
		return Route{Kind: KindNew}, true
	case len(seg) == 1 && seg[0] == "search":
		return Route{Kind: KindSearch}, true
	case len(seg) == 1 && seg[0] == "search.xml":
		return Route{Kind: KindOpenSearch}, true
	case len(seg) == 2 && seg[0] == "editions":
		if id, ok := parseID(seg[1]); ok {
			return Route{Kind: KindEdition, EditionID: id}, true
		}
	case len(seg) == 2 && seg[0] == "works":
		if id, ok := parseID(seg[1]); ok {
			return Route{Kind: KindWork, WorkID: id}, true
		}
	case len(seg) == 4 && seg[0] == "works" && seg[2] == "chapters":
		wid, ok1 := parseID(seg[1])
		cid, ok2 := parseID(seg[3])
		if ok1 && ok2 {
			return Route{Kind: KindChapter, WorkID: wid, ChapterID: cid}, true
		}
	}
	return Route{}, false
}

// parseID — только положительное целое из цифр: «+5», «05x», «-1» — не адрес
// каталога. Ведущие нули не запрещены: «007» — тот же том 7.
func parseID(s string) (int64, bool) {
	if s == "" || strings.TrimLeft(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}

func editionPath(id int64) string { return "/opds/editions/" + strconv.FormatInt(id, 10) }
func workPath(id int64) string    { return "/opds/works/" + strconv.FormatInt(id, 10) }
func chapterPath(workID, chapterID int64) string {
	return workPath(workID) + "/chapters/" + strconv.FormatInt(chapterID, 10)
}
