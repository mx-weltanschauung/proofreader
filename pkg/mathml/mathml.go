// Package mathml превращает формулы TeX в MathML, исполняя настоящий KaTeX
// внутри процесса: JS-движок goja — чистый Go, без cgo и без внешних служб.
//
// Пакет знает только про формулы. Ни про книги, ни про писателей, ни про
// HTTP он не осведомлён — это и позволяет держать его под тестами целиком.
package mathml

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// katexJS — вендорная копия KaTeX. Обновляется вместе с фронтом:
//
//	cp frontend/node_modules/katex/dist/katex.min.js pkg/mathml/katex.min.js
//
// Разъезд копий ловит TestВерсияСовпадаетСФронтом. katex.LICENSE рядом —
// текст MIT-лицензии KaTeX (minified dist сам баннер не несёт); обновляется
// той же командой, только cp берёт LICENSE вместо katex.min.js:
//
//	cp frontend/node_modules/katex/LICENSE pkg/mathml/katex.LICENSE
//
//go:embed katex.min.js
var katexJS string

var (
	programOnce sync.Once
	program     *goja.Program
	programErr  error
)

// pool хранит готовые рантаймы. Это не оптимизация, а требование
// корректности: goja.Runtime не потокобезопасен, а скачивания идут в разных
// горутинах. Побочно снимает 56 мс загрузки KaTeX с каждой формулы — рендер
// сам по себе стоит 13 мс.
var pool sync.Pool

type runtime struct {
	rt     *goja.Runtime
	render goja.Callable
}

func compile() (*goja.Program, error) {
	programOnce.Do(func() {
		program, programErr = goja.Compile("katex.min.js", katexJS, true)
	})
	return program, programErr
}

func newRuntime() (*runtime, error) {
	prog, err := compile()
	if err != nil {
		return nil, fmt.Errorf("mathml: компиляция katex: %w", err)
	}
	rt := goja.New()
	// UMD-сборка KaTeX ищет window/module/exports. Даём ей глобальный объект
	// и явно гасим CommonJS, иначе она уедет в module.exports и не объявит
	// себя глобально.
	if _, err := rt.RunString(`var window = this; var module = undefined; var exports = undefined;`); err != nil {
		return nil, fmt.Errorf("mathml: подготовка окружения: %w", err)
	}
	if _, err := rt.RunProgram(prog); err != nil {
		return nil, fmt.Errorf("mathml: загрузка katex: %w", err)
	}
	katex := rt.Get("katex")
	if katex == nil || goja.IsUndefined(katex) {
		return nil, fmt.Errorf("mathml: katex не объявил себя глобально")
	}
	fn, ok := goja.AssertFunction(katex.ToObject(rt).Get("renderToString"))
	if !ok {
		return nil, fmt.Errorf("mathml: katex.renderToString — не функция")
	}
	return &runtime{rt: rt, render: fn}, nil
}

func acquire() (*runtime, error) {
	if r, ok := pool.Get().(*runtime); ok && r != nil {
		return r, nil
	}
	return newRuntime()
}

// Render превращает формулу в элемент MathML. display — блочная формула,
// то есть написанная в тексте как $$…$$.
func Render(tex string, display bool) (string, error) {
	r, err := acquire()
	if err != nil {
		return "", err
	}
	defer pool.Put(r)

	opts := r.rt.NewObject()
	if err := opts.Set("displayMode", display); err != nil {
		return "", fmt.Errorf("mathml: настройка displayMode: %w", err)
	}
	if err := opts.Set("output", "mathml"); err != nil {
		return "", fmt.Errorf("mathml: настройка output: %w", err)
	}
	// throwOnError: true — вопреки фронту, где стоит false. На сайте сломанная
	// формула не должна рушить страницу; здесь ошибка нужна вызывающему как
	// сигнал оставить исходный LaTeX.
	if err := opts.Set("throwOnError", true); err != nil {
		return "", fmt.Errorf("mathml: настройка throwOnError: %w", err)
	}

	res, err := r.render(goja.Undefined(), r.rt.ToValue(tex), opts)
	if err != nil {
		return "", fmt.Errorf("mathml: формула %q: %w", tex, err)
	}
	return extractMath(res.String())
}

// Version — версия встроенного KaTeX.
func Version() (string, error) {
	r, err := acquire()
	if err != nil {
		return "", err
	}
	defer pool.Put(r)

	v, err := r.rt.RunString("katex.version")
	if err != nil {
		return "", fmt.Errorf("mathml: чтение katex.version: %w", err)
	}
	return v.String(), nil
}

// extractMath вынимает элемент math из обёртки KaTeX (<span class="katex">).
// Спан — вспомогательная разметка читальни, в скачиваемый файл ей незачем.
func extractMath(rendered string) (string, error) {
	start := strings.Index(rendered, "<math")
	end := strings.LastIndex(rendered, "</math>")
	if start < 0 || end < start {
		return "", fmt.Errorf("mathml: в выводе katex нет элемента math")
	}
	return rendered[start : end+len("</math>")], nil
}
