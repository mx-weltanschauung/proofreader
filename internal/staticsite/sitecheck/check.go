// Package sitecheck проверяет готовый каталог статической читальни. Он
// намеренно не импортирует генератор (internal/staticsite): проверка,
// повторяющая логику генератора, повторяла бы и его ошибки. Только разбор
// готового HTML и сверка с базой (Options.Expected).
package sitecheck

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type Options struct {
	Dir string
	// AllowedExternal — префикс внешних адресов, допустимых в <a href>
	// («https://lib.example.org/»). Ресурсы снаружи недопустимы никогда.
	AllowedExternal string
	// Expected — печатные номера непустых полос каждой работы по базе; nil —
	// полноту не проверять.
	Expected map[int64][]int
	// Excluded — работы, которых в сборке быть не должно (снятые по жалобе,
	// scripts/static-exclude.txt): их каталог works/<id>[-слаг]/ — расхождение.
	Excluded []int64
	// Allowed — работы, которым можно быть в сборке: каталог боевого вместе с
	// передними листами; nil — не проверять. Каталог works/<id>[-слаг]/ работы
	// вне списка — расхождение: в архив попал том, которого на боевом нет.
	Allowed map[int64]bool
	// Forbidden — печатные номера полос, которых в сборке быть не должно:
	// снятый аппарат («N apparatus» в scripts/static-exclude.txt).
	Forbidden map[int64][]int
	// OnProd — печатные номера полос, которые есть на боевом, у работ со
	// снятым аппаратом. Полоса такой работы, которой на боевом нет, —
	// расхождение: Forbidden считается по локальной разметке is_apparatus, а
	// она бывает другой, чем та, по которой снимали на боевом.
	OnProd map[int64][]int
}

type Problem struct{ File, Message string }

func (p Problem) String() string { return p.File + ": " + p.Message }

type ref struct{ file, tag, value string }

type checker struct {
	opt      Options
	ids      map[string]map[string]bool
	refs     []ref
	problems []Problem
}

// Каталог pagefind — чужой код и бинарные куски индекса, его не разбираем.
const pagefindDir = "pagefind"

var leaks = []string{"/api/", "X-Reader-Key"}

func Run(opt Options) ([]Problem, error) {
	c := &checker{opt: opt, ids: map[string]map[string]bool{}}
	if err := filepath.WalkDir(opt.Dir, c.visit); err != nil {
		return nil, err
	}
	c.checkRefs()
	c.checkCompleteness()
	c.checkExcluded()
	c.checkAllowed()
	c.checkForbidden()
	c.checkOnProd()
	sort.Slice(c.problems, func(i, j int) bool {
		if c.problems[i].File != c.problems[j].File {
			return c.problems[i].File < c.problems[j].File
		}
		return c.problems[i].Message < c.problems[j].Message
	})
	return c.problems, nil
}

func (c *checker) problem(file, format string, args ...any) {
	c.problems = append(c.problems, Problem{File: file, Message: fmt.Sprintf(format, args...)})
}

func (c *checker) visit(full string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	relPath, err := filepath.Rel(c.opt.Dir, full)
	if err != nil {
		return err
	}
	relPath = filepath.ToSlash(relPath)
	if d.IsDir() {
		if relPath == pagefindDir {
			return filepath.SkipDir
		}
		return nil
	}
	ext := path.Ext(relPath)
	if ext != ".html" && ext != ".css" && ext != ".js" {
		return nil
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	for _, l := range leaks {
		if bytes.Contains(data, []byte(l)) {
			c.problem(relPath, "утечка: в файле есть %q", l)
		}
	}
	switch ext {
	case ".html":
		return c.parseHTML(relPath, data)
	case ".css":
		c.checkCSS(relPath, string(data))
	}
	return nil
}

var linkAttrs = map[string]string{
	"a": "href", "link": "href", "script": "src", "img": "src",
	"form": "action", "iframe": "src", "source": "src",
}

func (c *checker) parseHTML(file string, data []byte) error {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	ids := map[string]bool{}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			want := linkAttrs[n.Data]
			for _, a := range n.Attr {
				if a.Key == "id" {
					ids[a.Val] = true
				}
				if want != "" && a.Key == want {
					c.refs = append(c.refs, ref{file: file, tag: n.Data, value: a.Val})
				}
			}
			if n.Data == "style" && n.FirstChild != nil {
				c.checkCSS(file, n.FirstChild.Data)
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	c.ids[file] = ids
	return nil
}

var cssURL = regexp.MustCompile(`url\(\s*['"]?([^'")]+)`)

func (c *checker) checkCSS(file, css string) {
	if strings.Contains(css, "@import") {
		c.problem(file, "@import в CSS: офлайн-копия грузит стили одним файлом")
	}
	for _, m := range cssURL.FindAllStringSubmatch(css, -1) {
		v := strings.TrimSpace(m[1])
		if strings.HasPrefix(v, "data:") {
			continue
		}
		if hasScheme(v) || strings.HasPrefix(v, "/") {
			c.problem(file, "внешний адрес в CSS: %s — офлайн-копия не должна ходить в сеть", v)
			continue
		}
		c.checkRelative(ref{file: file, tag: "css", value: v})
	}
}

func hasScheme(v string) bool {
	u, err := url.Parse(v)
	return err == nil && u.Scheme != ""
}

func (c *checker) checkRefs() {
	for _, r := range c.refs {
		v := strings.TrimSpace(r.value)
		switch {
		case v == "":
			c.problem(r.file, "пустая ссылка у <%s>", r.tag)
		case strings.HasPrefix(v, "#"):
			if !c.ids[r.file][v[1:]] {
				c.problem(r.file, "нет якоря %s", v)
			}
		case strings.HasPrefix(v, "/"):
			c.problem(r.file, "абсолютный путь %s: с диска не откроется", v)
		case hasScheme(v):
			if r.tag == "a" && c.opt.AllowedExternal != "" && strings.HasPrefix(v, c.opt.AllowedExternal) {
				continue
			}
			c.problem(r.file, "внешний адрес %s у <%s>: офлайн-копия не должна ходить в сеть", v, r.tag)
		default:
			c.checkRelative(r)
		}
	}
}

func (c *checker) checkRelative(r ref) {
	target, frag, _ := strings.Cut(r.value, "#")
	target, _, _ = strings.Cut(target, "?")
	if unescaped, err := url.PathUnescape(target); err == nil {
		target = unescaped
	}
	if strings.HasSuffix(target, "/") {
		c.problem(r.file, "ссылка на каталог %s: с диска браузер не подставит index.html", target)
		return
	}
	joined := path.Join(path.Dir(r.file), target)
	if joined == ".." || strings.HasPrefix(joined, "../") {
		c.problem(r.file, "ссылка %s ведёт за пределы каталога", r.value)
		return
	}
	info, err := os.Stat(filepath.Join(c.opt.Dir, filepath.FromSlash(joined)))
	if err != nil {
		c.problem(r.file, "нет файла %s", joined)
		return
	}
	if info.IsDir() {
		c.problem(r.file, "ссылка на каталог %s: с диска браузер не подставит index.html", joined)
		return
	}
	if frag != "" && strings.HasSuffix(joined, ".html") && !c.ids[joined][frag] {
		c.problem(r.file, "в %s нет якоря #%s", joined, frag)
	}
}

// pageAnchors — печатные номера полос (якоря p<n>) в файлах каждой работы
// (works/<id>[-слаг]/).
func (c *checker) pageAnchors() map[int64]map[int]bool {
	found := map[int64]map[int]bool{}
	for file, ids := range c.ids {
		work, ok := workOfFile(file)
		if !ok {
			continue
		}
		for id := range ids {
			if n, ok := printedOf(id); ok {
				if found[work] == nil {
					found[work] = map[int]bool{}
				}
				found[work][n] = true
			}
		}
	}
	return found
}

// checkCompleteness сверяет якоря полос p<n> в файлах каждой работы с
// полосами базы.
func (c *checker) checkCompleteness() {
	if c.opt.Expected == nil {
		return
	}
	found := c.pageAnchors()
	works := make([]int64, 0, len(c.opt.Expected))
	for w := range c.opt.Expected {
		works = append(works, w)
	}
	sort.Slice(works, func(i, j int) bool { return works[i] < works[j] })
	for _, w := range works {
		var missing []int
		for _, n := range c.opt.Expected[w] {
			if !found[w][n] {
				missing = append(missing, n)
			}
		}
		if len(missing) == 0 {
			continue
		}
		shown := missing
		if len(shown) > 10 {
			shown = shown[:10]
		}
		c.problem(fmt.Sprintf("works/%d", w), "нет %d полос из %d, например: %v", len(missing), len(c.opt.Expected[w]), shown)
	}
}

// checkExcluded ищет файлы снятых работ: ни одного быть не должно.
func (c *checker) checkExcluded() {
	excluded := map[int64]bool{}
	for _, id := range c.opt.Excluded {
		excluded[id] = true
	}
	seen := map[int64]bool{}
	for file := range c.ids {
		if work, ok := workOfFile(file); ok && excluded[work] && !seen[work] {
			seen[work] = true
			c.problem(fmt.Sprintf("works/%d", work), "снятая работа попала в архив")
		}
	}
}

// checkAllowed ищет работы вне списка боевого.
func (c *checker) checkAllowed() {
	if c.opt.Allowed == nil {
		return
	}
	seen := map[int64]bool{}
	for file := range c.ids {
		if work, ok := workOfFile(file); ok && !c.opt.Allowed[work] && !seen[work] {
			seen[work] = true
			c.problem(fmt.Sprintf("works/%d", work), "работы нет в списке боевого, а она в архиве")
		}
	}
}

// checkForbidden ищет полосы снятого аппарата: ни одного якоря p<n> из
// Forbidden в файлах работы быть не должно.
func (c *checker) checkForbidden() {
	if len(c.opt.Forbidden) == 0 {
		return
	}
	found := c.pageAnchors()
	for work, nums := range c.opt.Forbidden {
		var hit []int
		for _, n := range nums {
			if found[work][n] {
				hit = append(hit, n)
			}
		}
		if len(hit) > 0 {
			sort.Ints(hit)
			c.problem(fmt.Sprintf("works/%d", work), "в архиве полосы снятого аппарата: %v", hit)
		}
	}
}

// checkOnProd ищет у работ из OnProd полосы, которых на боевом нет.
func (c *checker) checkOnProd() {
	if len(c.opt.OnProd) == 0 {
		return
	}
	found := c.pageAnchors()
	for work, nums := range c.opt.OnProd {
		live := map[int]bool{}
		for _, n := range nums {
			live[n] = true
		}
		var gone []int
		for n := range found[work] {
			if !live[n] {
				gone = append(gone, n)
			}
		}
		if len(gone) > 0 {
			sort.Ints(gone)
			c.problem(fmt.Sprintf("works/%d", work), "в архиве полосы, которых на боевом нет (снятый аппарат размечен локально иначе?): %v", gone)
		}
	}
}

func workOfFile(file string) (int64, bool) {
	rest, ok := strings.CutPrefix(file, "works/")
	if !ok {
		return 0, false
	}
	dir, _, ok := strings.Cut(rest, "/")
	if !ok {
		return 0, false
	}
	idPart, _, _ := strings.Cut(dir, "-")
	id, err := strconv.ParseInt(idPart, 10, 64)
	return id, err == nil
}

func printedOf(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "p")
	if !ok || rest == "" {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil
}
