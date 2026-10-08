package staticsite

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ExcludeList — что не идёт в архив (scripts/static-exclude.txt).
type ExcludeList struct {
	// Works — работы целиком, вместе с их передними листами.
	Works map[int64]bool
	// Apparatus — работы, у которых снят только аппарат (takedown.sh
	// apparatus): тело идёт в архив, аппарат — нет.
	Apparatus map[int64]bool
}

// ParseExcludeList читает список работ, которые в архив не идут
// (scripts/static-exclude.txt): по номеру работы в начале строки, после «#»
// — комментарий. Нужен из-за снятия по жалобе: снятие уносит том с боевого,
// а в локальной базе, из которой собирается архив, он остаётся
// (scripts/takedown.sh), и розданный архив назад уже не забрать.
//
// «N» — работа целиком, «N apparatus» — только её аппарат.
//
// Строка без номера или с другим словом после него — ошибка, а не пропуск:
// опечатка в этом списке молча вернула бы снятое в архив («aparatus» не
// должен ни исключить том целиком, ни не исключить ничего).
func ParseExcludeList(r io.Reader) (ExcludeList, error) {
	out := ExcludeList{Works: map[int64]bool{}, Apparatus: map[int64]bool{}}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line, _, _ := strings.Cut(sc.Text(), "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil || id <= 0 {
			return ExcludeList{}, fmt.Errorf("строка %d: %q — нужен номер работы", n, sc.Text())
		}
		switch {
		case len(fields) == 1:
			out.Works[id] = true
		case len(fields) == 2 && fields[1] == "apparatus":
			out.Apparatus[id] = true
		default:
			return ExcludeList{}, fmt.Errorf("строка %d: %q — после номера работы ждали конец строки или слово apparatus", n, sc.Text())
		}
	}
	return out, sc.Err()
}
