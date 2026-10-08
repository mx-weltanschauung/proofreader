package staticsite

import (
	"strings"
	"testing"
)

func TestParseOnlyList(t *testing.T) {
	got, err := ParseOnlyList(strings.NewReader(`[{"id": 49, "title": "Ленин. Том 6", "slug": "lenin-t06"}, {"id": 318, "title": "Горький. Том 28"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (OnlyWork{ID: 49, Title: "Ленин. Том 6"}) || got[1].ID != 318 {
		t.Errorf("список = %+v", got)
	}
	for _, bad := range []string{`[]`, `null`, `{"id": 1}`, `[{"title": "без номера"}]`, `не json`} {
		if _, err := ParseOnlyList(strings.NewReader(bad)); err == nil {
			t.Errorf("%s принят — сборка пошла бы по пустому или битому списку боевого", bad)
		}
	}
}

func TestParseProdPages(t *testing.T) {
	got, err := ParseProdPages(strings.NewReader(`{"288": [1, 2, 381], "145": []}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[288]) != 3 || got[288][2] != 381 || got[145] == nil {
		t.Errorf("полосы боевого = %v", got)
	}
	for _, bad := range []string{`[]`, `{"сто": [1]}`, `{"0": [1]}`, `не json`} {
		if _, err := ParseProdPages(strings.NewReader(bad)); err == nil {
			t.Errorf("%s принят — сверка с боевым шла бы по битому файлу", bad)
		}
	}
}

func TestCheckOnlyList(t *testing.T) {
	only := []OnlyWork{{ID: 49, Title: "Ленин. Том 6"}, {ID: 318, Title: "Горький. Том 28 "}}
	if err := CheckOnlyList(only, map[int64]string{49: "Ленин. Том 6", 318: "Горький. Том 28", 7: "лишний"}); err != nil {
		t.Errorf("совпадающий список отвергнут: %v", err)
	}
	err := CheckOnlyList(only, map[int64]string{49: "Плеханов. Том 6"})
	if err == nil {
		t.Fatal("расхождение принято молча")
	}
	for _, want := range []string{"работа 49: на боевом «Ленин. Том 6», локально «Плеханов. Том 6»", "работа 318 («Горький. Том 28 ») есть на боевом, а локально нет"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в ошибке нет %q:\n%v", want, err)
		}
	}
}
