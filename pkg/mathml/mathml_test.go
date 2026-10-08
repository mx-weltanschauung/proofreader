package mathml

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// schemaTeX — схема воспроизводства со страницы 72 работы 55: две строки в
// фигурной скобке. Ради неё всё и затевалось.
const schemaTeX = `\left.\begin{array}{l}\text{I } 4000\,c + 1000\,v + 1000\,m = 6000\\` +
	`\text{II } 2000\,c + 500\,v + 500\,m = 3000\end{array}\right\}` +
	`\begin{array}{l}\text{Капитал} = 7500\\\text{Продукт} = 9000\end{array}`

func TestRenderОтдаётMathMLСоСкобкой(t *testing.T) {
	got, err := Render(schemaTeX, true)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.HasPrefix(got, "<math") || !strings.HasSuffix(got, "</math>") {
		t.Fatalf("ожидался голый элемент math, получено: %.120s…", got)
	}
	if strings.Contains(got, `class="katex"`) {
		t.Error("обёртка KaTeX должна быть снята: в файл едет формула, а не спан читальни")
	}
	for _, want := range []string{"<mtable", `<mo fence="true">}</mo>`, `display="block"`, "Капитал"} {
		if !strings.Contains(got, want) {
			t.Errorf("в MathML нет %q", want)
		}
	}
}

func TestRenderСтрочнойФормулы(t *testing.T) {
	got, err := Render(`I(v+m) = II\,c`, false)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, `display="block"`) {
		t.Error("строчная формула не должна быть блочной")
	}
}

func TestRenderБитойФормулыВозвращаетОшибку(t *testing.T) {
	// throwOnError=true поставлен намеренно: писателю нужен сигнал к
	// деградации, а не красный «ParseError» в разметке файла.
	if _, err := Render(`\frac{1`, true); err == nil {
		t.Fatal("ожидалась ошибка на незакрытой скобке")
	}
}

func TestВерсияСовпадаетСФронтом(t *testing.T) {
	got, err := Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "package.json"))
	if err != nil {
		t.Fatalf("package.json: %v", err)
	}
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("разбор package.json: %v", err)
	}
	want := strings.TrimLeft(pkg.Dependencies["katex"], "^~")
	if got != want {
		t.Fatalf("вендорный KaTeX %s, во фронте %s — копии разъехались; обнови "+
			"pkg/mathml/katex.min.js из frontend/node_modules", got, want)
	}
}

func TestПараллельныйРендер(t *testing.T) {
	// goja.Runtime не потокобезопасен; пул рантаймов существует ради этого.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Render(`a^2 + b^2 = c^2`, false); err != nil {
				t.Errorf("Render: %v", err)
			}
		}()
	}
	wg.Wait()
}
