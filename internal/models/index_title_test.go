package models

import (
	"encoding/json"
	"os"
	"testing"
)

// Таблица «заголовок → ключ» лежит в testdata/index_title_keys.json, а не
// здесь: по ней же проверяется питоновский близнец
// (tools/ocr_ingest/lenin_index_stitch.normalize_title,
// test_lenin_index_stitch.TestNormalizeTitleTwin). Правило ключа меняют оба
// сразу, и разойтись им не даёт одна и та же таблица, а не комментарий.
func TestNormalizeIndexTitle(t *testing.T) {
	raw, err := os.ReadFile("testdata/index_title_keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		In  string `json:"in"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("таблица ключей пуста")
	}
	for _, c := range cases {
		if got := NormalizeIndexTitle(c.In); got != c.Key {
			t.Errorf("NormalizeIndexTitle(%q) = %q, хотели %q", c.In, got, c.Key)
		}
	}
}
