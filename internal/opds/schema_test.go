package opds

import (
	"bytes"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Схемная проверка лент — приёмом pkg/book/fb2_schema_test.go: lxml и
// вендоренная схема (testdata/schema, README там же). Unmarshal в тестах
// обработчика проверяет только, что XML разбирается, а не что это лента
// OPDS: без updated у записи, с пробелом в href она разбирается так же.
func validatorPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к тестовому файлу")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata", "schema", "validate.py")
}

func requireValidator(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 не найден — схемная проверка OPDS пропущена")
	}
	if err := exec.Command("python3", "-c", "import lxml.etree").Run(); err != nil {
		t.Skip("python3-lxml не установлен — схемная проверка OPDS пропущена")
	}
}

func validate(t *testing.T, doc string) error {
	t.Helper()
	cmd := exec.Command("python3", validatorPath(t))
	cmd.Stdin = strings.NewReader(doc)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &schemaError{msg: stderr.String()}
	}
	return nil
}

type schemaError struct{ msg string }

func (e *schemaError) Error() string { return e.msg }

func TestEveryFeedMatchesOPDSSchema(t *testing.T) {
	requireValidator(t)
	h := NewHandler(fixtureSource(), testBase)
	for _, target := range []string{
		"/opds",
		"/opds/editions",
		"/opds/editions/7",
		"/opds/loose",
		"/opds/new",
		"/opds/works/252",
		"/opds/works/252/chapters/10",
		"/opds/works/252/chapters/20",
		"/opds/works/252/chapters/30", // лист: лента из одного «целиком»
		"/opds/works/253/chapters/60?page=2",
		"/opds/search?q=xx",
	} {
		rec := serve(t, h, target)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d", target, rec.Code)
			continue
		}
		if err := validate(t, rec.Body.String()); err != nil {
			t.Errorf("%s не проходит схему OPDS:\n%v\n%s", target, err, rec.Body.String())
		}
	}
}

// Заглавие из распознавания бывает с управляющим знаком (U+0001 — тот же,
// которым поиск метит границы совпадения). В XML 1.0 он недопустим вовсе:
// лента обязана остаться валидной, а не стать неразбираемой целиком.
func TestControlCharsInTitlesKeepFeedValid(t *testing.T) {
	requireValidator(t)
	src := fixtureSource()
	tree := src.trees[252]
	tree.Volume.Title = "Том\x01 1"
	tree.Nodes[0].Title = "1901\x0b–1907"
	rec := serve(t, NewHandler(src, testBase), "/opds/works/252")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if err := validate(t, rec.Body.String()); err != nil {
		t.Fatalf("лента с управляющими знаками в заглавиях не прошла схему: %v", err)
	}
}

// Валидатор обязан уметь сказать «нет» — иначе зелёный тест выше ничего не
// значит.
func TestSchemaValidatorRejectsBrokenFeed(t *testing.T) {
	requireValidator(t)
	good := serve(t, NewHandler(fixtureSource(), testBase), "/opds/works/252").Body.String()
	if err := validate(t, good); err != nil {
		t.Fatalf("исходная лента должна проходить: %v", err)
	}
	for name, broken := range map[string]string{
		"пробел в href":   strings.Replace(good, "/og/work/252.png", "/og/work 252.png", 1),
		"запись без id":   strings.Replace(good, "<id>tag:lib.example.org,2026:work:252</id>", "", 1),
		"не тот корень":   strings.Replace(strings.Replace(good, "<feed ", "<fead ", 1), "</feed>", "</fead>", 1),
		"updated не дата": strings.Replace(good, "<updated>2026-09-03T12:00:00Z</updated>", "<updated>вчера</updated>", 1),
	} {
		if broken == good {
			t.Fatalf("%s: замена не сработала — тест проверял бы исходную ленту", name)
		}
		if validate(t, broken) == nil {
			t.Errorf("%s: схема пропустила испорченную ленту", name)
		}
	}
}
