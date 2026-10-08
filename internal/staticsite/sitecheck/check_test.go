package sitecheck

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func good() map[string]string {
	return map[string]string{
		"index.html":           `<a href="works/1-a/index.html">т</a><a href="works/1-a/2-b.html#ch-2">г</a><a href="https://lib.example.org/">онлайн</a><form action="search/index.html"></form><link rel="stylesheet" href="assets/site.css">`,
		"works/1-a/index.html": `<a href="../../index.html">д</a>`,
		"works/1-a/2-b.html":   `<h1 id="ch-2">г</h1><span id="p11"></span><span id="p12"></span><a href="#p12">к</a>`,
		"search/index.html":    `<script src="../assets/titles.js"></script>`,
		"assets/site.css":      `@font-face { src: url(fonts/L.ttf); }`,
		"assets/fonts/L.ttf":   "x",
		"assets/titles.js":     "window.TITLES=[];",
		"pagefind/pagefind.js": "/api/ внутри pagefind не проверяется",
	}
}

func run(t *testing.T, files map[string]string, expected map[int64][]int) []Problem {
	t.Helper()
	problems, err := Run(Options{Dir: writeTree(t, files), AllowedExternal: "https://lib.example.org/", Expected: expected})
	if err != nil {
		t.Fatal(err)
	}
	return problems
}

func TestCleanTreeHasNoProblems(t *testing.T) {
	if p := run(t, good(), map[int64][]int{1: {11, 12}}); len(p) != 0 {
		t.Fatalf("чистое дерево дало расхождения: %v", p)
	}
}

func expectProblem(t *testing.T, problems []Problem, fragment string) {
	t.Helper()
	for _, p := range problems {
		if strings.Contains(p.String(), fragment) {
			return
		}
	}
	t.Errorf("нет расхождения с %q среди %v", fragment, problems)
}

func TestDetectsBrokenThings(t *testing.T) {
	cases := []struct {
		name, file, body, want string
	}{
		{"нет файла", "a.html", `<a href="nope.html">x</a>`, "нет файла nope.html"},
		{"нет якоря в чужом файле", "a.html", `<a href="works/1-a/2-b.html#p99">x</a>`, "нет якоря #p99"},
		{"нет якоря в своём", "a.html", `<a href="#zzz">x</a>`, "нет якоря #zzz"},
		{"абсолютный путь", "a.html", `<a href="/works/1">x</a>`, "абсолютный путь"},
		{"ссылка на каталог", "a.html", `<a href="works/1-a/">x</a>`, "каталог"},
		{"внешний скрипт", "a.html", `<script src="https://cdn.example/x.js"></script>`, "внешний адрес"},
		{"чужой внешний href", "a.html", `<a href="https://example.org/">x</a>`, "внешний адрес"},
		{"за пределы", "a.html", `<a href="../x.html">x</a>`, "за пределы"},
		{"утечка api", "a.html", `<p>/api/works</p>`, "/api/"},
		{"внешний css", "assets/x.css", `body { background: url(https://example.org/x.png); }`, "внешний адрес"},
		{"импорт css", "assets/y.css", `@import "z.css";`, "@import"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := good()
			files[c.file] = c.body
			expectProblem(t, run(t, files, nil), c.want)
		})
	}
}

func TestDetectsMissingPages(t *testing.T) {
	problems := run(t, good(), map[int64][]int{1: {11, 12, 13}, 2: {5}})
	expectProblem(t, problems, "works/1: нет 1 полос из 3, например: [13]")
	expectProblem(t, problems, "works/2: нет 1 полос из 1")
}

func TestDetectsExcludedWork(t *testing.T) {
	problems, err := Run(Options{Dir: writeTree(t, good()), AllowedExternal: "https://lib.example.org/", Excluded: []int64{1, 7}})
	if err != nil {
		t.Fatal(err)
	}
	expectProblem(t, problems, "works/1: снятая работа попала в архив")
	for _, p := range problems {
		if strings.HasPrefix(p.File, "works/7") {
			t.Errorf("работы 7 в сборке нет, а расхождение есть: %v", p)
		}
	}
}

func TestDoesNotImportGenerator(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range pkg.Imports {
		if imp == "proofreader/internal/staticsite" {
			t.Fatal("самопроверка импортирует генератор и повторит его ошибки")
		}
	}
}

func TestAllowedFlagsWorkMissingOnProd(t *testing.T) {
	dir := writeTree(t, good())
	got, err := Run(Options{Dir: dir, AllowedExternal: "https://lib.example.org/", Allowed: map[int64]bool{2: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].File != "works/1" || !strings.Contains(got[0].Message, "боевого") {
		t.Errorf("работа 1 вне списка боевого не замечена: %v", got)
	}
	got, err = Run(Options{Dir: dir, AllowedExternal: "https://lib.example.org/", Allowed: map[int64]bool{1: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("работа 1 в списке, а проверка ругается: %v", got)
	}
}

func TestForbiddenFlagsApparatusPages(t *testing.T) {
	dir := writeTree(t, good())
	got, err := Run(Options{Dir: dir, AllowedExternal: "https://lib.example.org/", Forbidden: map[int64][]int{1: {13, 12}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].File != "works/1" || !strings.Contains(got[0].Message, "[12]") {
		t.Errorf("полоса 12 снятого аппарата не замечена: %v", got)
	}
	got, err = Run(Options{Dir: dir, AllowedExternal: "https://lib.example.org/", Forbidden: map[int64][]int{1: {13}, 2: {12}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("полос 13 и чужой 12 в сборке нет, а проверка ругается: %v", got)
	}
}

// OnProd — полосы, которые есть на боевом: самопроверка сверяет сборку с ними,
// а не только с локальной разметкой аппарата, которая бывает другой.
func TestOnProdFlagsPagesGoneFromProd(t *testing.T) {
	dir := writeTree(t, good())
	got, err := Run(Options{Dir: dir, AllowedExternal: "https://lib.example.org/", OnProd: map[int64][]int{1: {11}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].File != "works/1" || !strings.Contains(got[0].Message, "[12]") {
		t.Errorf("полоса 12, которой нет на боевом, не замечена: %v", got)
	}
	got, err = Run(Options{Dir: dir, AllowedExternal: "https://lib.example.org/", OnProd: map[int64][]int{1: {11, 12, 13}, 2: {1}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("все полосы есть на боевом, а проверка ругается: %v", got)
	}
}
