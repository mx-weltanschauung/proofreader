package stats

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// leadingID — ключ сегмента вида «49-lenin-t06»: ведущее целое, хвост после
// дефиса игнорируется. Копия seo.leadingID: импортировать seo отсюда нельзя
// (seo → repository → stats — цикл). Правится в обоих местах сразу.
func leadingID(seg string) (int64, bool) {
	end := 0
	for end < len(seg) && seg[end] >= '0' && seg[end] <= '9' {
		end++
	}
	if end == 0 || (end < len(seg) && seg[end] != '-') {
		return 0, false
	}
	v, err := strconv.ParseInt(seg[:end], 10, 64)
	return v, err == nil && v > 0
}

// maxPathLen — длиннее пути в читальне не бывает; всё сверх — мусор.
const maxPathLen = 512

// Target — сущность, которую показывает путь SPA.
type Target struct {
	Kind     string
	WorkID   int64
	EntityID int64
	SlugKey  string
}

// служебные сегменты, после которых путь — форма, а не чтение.
var formSegments = map[string]bool{"new": true, "edit": true, "suggest": true}

// ParsePath разбирает путь SPA (frontend/src/App.tsx) в сущность. false —
// путь не считается: служебный, форма правки или мусор.
func ParsePath(path string) (Target, bool) {
	if len(path) > maxPathLen || !strings.HasPrefix(path, "/") {
		return Target{}, false
	}
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return Target{Kind: "home"}, true
	}
	p := strings.Split(trimmed, "/")
	for i, seg := range p {
		// react-router отдаёт путь закодированным при первой загрузке и
		// декодированным после перехода внутри SPA: без раскодирования одна
		// сущность легла бы под двумя ключами.
		dec, err := url.PathUnescape(seg)
		if err != nil || dec == "" || formSegments[dec] {
			return Target{}, false
		}
		p[i] = dec
	}
	id := func(i int) (int64, bool) { return leadingID(p[i]) }

	switch p[0] {
	case "search", "help":
		if len(p) == 1 {
			return Target{Kind: p[0]}, true
		}
	case "legal", "feedback":
		if len(p) == 1 {
			return Target{Kind: "other", SlugKey: p[0]}, true
		}
	case "editions":
		if len(p) == 2 {
			if n, ok := id(1); ok {
				return Target{Kind: "edition", EntityID: n}, true
			}
		}
	case "works":
		return parseWork(p, id)
	case "documents", "collections", "concepts":
		if len(p) == 1 {
			return Target{Kind: "list", SlugKey: p[0]}, true
		}
		kind := map[string]string{"documents": "document", "collections": "collection", "concepts": "concept"}[p[0]]
		switch {
		case len(p) == 2 && validSlug(p[1]):
			return Target{Kind: kind, SlugKey: p[1]}, true
		case len(p) == 3 && p[0] != "concepts" && validSlug(p[1]) && validSlug(p[2]):
			return Target{Kind: kind, SlugKey: p[1] + "/" + p[2]}, true
		case len(p) == 4 && p[0] == "collections" && p[2] == "read" && validSlug(p[1]):
			if _, ok := id(3); ok {
				return Target{Kind: kind, SlugKey: p[1]}, true
			}
		case len(p) == 5 && p[0] == "collections" && p[3] == "read" && validSlug(p[1]) && validSlug(p[2]):
			if _, ok := id(4); ok {
				return Target{Kind: kind, SlugKey: p[1] + "/" + p[2]}, true
			}
		}
	}
	return Target{}, false
}

// maxSlugRunes — длиннее слага (ника) в читальне не бывает.
const maxSlugRunes = 100

// validSlug пускает в ключ свёртки только то, что похоже на слаг или ник:
// публичный /api/hit иначе позволял бы бесконечно плодить строки в
// навсегда хранимой свёртке.
func validSlug(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > maxSlugRunes {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func parseWork(p []string, id func(int) (int64, bool)) (Target, bool) {
	if len(p) < 2 {
		return Target{}, false
	}
	work, ok := id(1)
	if !ok {
		return Target{}, false
	}
	if len(p) == 2 {
		return Target{Kind: "work", WorkID: work}, true
	}
	if len(p) != 4 {
		return Target{}, false
	}
	kind := map[string]string{"chapters": "chapter", "pages": "page", "read": "read"}[p[2]]
	n, ok := id(3)
	if kind == "" || !ok {
		return Target{}, false
	}
	return Target{Kind: kind, WorkID: work, EntityID: n}, true
}
