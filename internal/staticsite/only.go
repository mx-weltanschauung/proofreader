package staticsite

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseProdPages читает полосы боевого у работ со снятым аппаратом (флаг
// -prod-pages cmd/staticsite check; файл пишет static-release.sh из
// GET /api/works/{id}/page-map): номер работы → внутренние номера полос.
// Самопроверка сверяет сборку с ними — независимо от локальной разметки
// is_apparatus, по которой сборка вырезала аппарат.
func ParseProdPages(r io.Reader) (map[int64][]int, error) {
	var raw map[string][]int
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("полосы боевого не разобраны: %w", err)
	}
	out := make(map[int64][]int, len(raw))
	for k, nums := range raw {
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("полосы боевого: %q — не номер работы", k)
		}
		if nums == nil {
			nums = []int{}
		}
		out[id] = nums
	}
	return out, nil
}

// OnlyWork — работа каталога боевого, как её отдаёт GET /api/works. Из ответа
// нужны только номер и заглавие.
type OnlyWork struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// ParseOnlyList читает список работ боевого (флаг -only cmd/staticsite;
// файл пишет static-release.sh). Пустой список — ошибка: архив из ничего
// собирать незачем, и пустой ответ скорее сбой, чем правда.
func ParseOnlyList(r io.Reader) ([]OnlyWork, error) {
	var list []OnlyWork
	if err := json.NewDecoder(r).Decode(&list); err != nil {
		return nil, fmt.Errorf("список работ боевого не разобран: %w", err)
	}
	if len(list) == 0 {
		return nil, errors.New("список работ боевого пуст")
	}
	for _, w := range list {
		if w.ID <= 0 {
			return nil, fmt.Errorf("в списке работ боевого работа без номера: %+v", w)
		}
	}
	return list, nil
}

// CheckOnlyList сверяет список боевого с локальной базой (local: id →
// заглавие). Сборка опирается на совпадение id томов локально и на боевом
// (docs/LOCAL_SCANS_WORKING_SET.md), и слепо ему не верит: заглавие —
// независимая сверка, что под одним номером один и тот же том. Работа боевого,
// которой локально нет, — тоже отказ: архив вышел бы неполным молча. Всё
// расхождение — одной ошибкой, списком.
func CheckOnlyList(only []OnlyWork, local map[int64]string) error {
	var bad []string
	for _, w := range only {
		title, ok := local[w.ID]
		switch {
		case !ok:
			bad = append(bad, fmt.Sprintf("работа %d («%s») есть на боевом, а локально нет", w.ID, w.Title))
		case strings.TrimSpace(title) != strings.TrimSpace(w.Title):
			bad = append(bad, fmt.Sprintf("работа %d: на боевом «%s», локально «%s»", w.ID, w.Title, title))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("состав боевого не сходится с локальной базой:\n  %s", strings.Join(bad, "\n  "))
	}
	return nil
}
