package api

import (
	"net/http"
	"strconv"
	"strings"
)

// parseIDList разбирает список идентификаторов через запятую («works=13,14»).
// Отсутствие параметра и пустая строка — nil и ok: область без этой оси.
// Любой нечисловой кусок или id меньше единицы — отказ целиком, а не молчаливый
// пропуск: читатель, пришедший по покорёженной ссылке, должен увидеть отказ, а
// не выдачу по случайно уцелевшей половине области.
//
// Повторы схлопываются: «works=13,13» — один том, и запрос не должен считать
// его дважды.
func parseIDList(r *http.Request, name string) ([]int64, bool) {
	raw := r.URL.Query().Get(name)
	if strings.TrimSpace(raw) == "" {
		return nil, true
	}
	seen := make(map[int64]bool)
	var out []int64
	for _, part := range strings.Split(raw, ",") {
		n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || n < 1 {
			return nil, false
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, true
}
