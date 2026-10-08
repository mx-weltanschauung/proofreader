package pagecache

import (
	"errors"
	"os"
	"testing"
	"time"
)

// statOf — маленький os.FileInfo с нужными полями, без реального файла на
// диске: тест подставляет размер и mtime напрямую, а не готовит файлы.
type statOf struct {
	size int64
	mod  time.Time
}

func (s statOf) Name() string       { return "" }
func (s statOf) Size() int64        { return s.size }
func (s statOf) Mode() os.FileMode  { return 0 }
func (s statOf) ModTime() time.Time { return s.mod }
func (s statOf) IsDir() bool        { return false }
func (s statOf) Sys() any           { return nil }

// Настоящий коммит подставлять незачем — resolveCommit его не трогает.
func TestResolveCommitKeepsRealCommit(t *testing.T) {
	got := resolveCommit("abc123", nil, nil, nil)
	if got != "abc123" {
		t.Errorf("resolveCommit = %q, ожидался настоящий коммит без изменений", got)
	}
}

// Само найденное поведение: два "unknown"-запуска с РАЗНЫМИ бинарниками (тут
// — разным размером файла, как после пересборки с другим рендерером) обязаны
// получить разные отметки. Без этого теста регрессия — вернуть
// pageCache.Init(Commit) без ResolveCommit — прошла бы незамеченной.
func TestResolveCommitDiffersAcrossRebuilds(t *testing.T) {
	exe := func() (string, error) { return "/app/server", nil }

	buildA := func(string) (os.FileInfo, error) {
		return statOf{size: 1000, mod: time.Unix(1, 0)}, nil
	}
	buildB := func(string) (os.FileInfo, error) {
		return statOf{size: 2000, mod: time.Unix(1, 0)}, nil
	}

	for _, commit := range []string{"", "unknown"} {
		a := resolveCommit(commit, exe, buildA, time.Now)
		b := resolveCommit(commit, exe, buildB, time.Now)
		if a == b {
			t.Errorf("commit=%q: отметки пересборок совпали (%q) — кэш прежней сборки пережил бы пересборку", commit, a)
		}
	}
}

// Обратное свойство: перезапуск ТОГО ЖЕ бинарника (тот же файл, тот же
// размер и mtime) обязан давать ту же отметку — иначе кэш не переживал бы
// обычный рестарт контейнера.
func TestResolveCommitStableAcrossRestarts(t *testing.T) {
	exe := func() (string, error) { return "/app/server", nil }
	stat := func(string) (os.FileInfo, error) {
		return statOf{size: 1234, mod: time.Unix(1700000000, 0)}, nil
	}

	first := resolveCommit("unknown", exe, stat, time.Now)
	second := resolveCommit("unknown", exe, stat, time.Now)
	if first != second {
		t.Errorf("отметки одного и того же бинарника разошлись: %q != %q", first, second)
	}
}

// Пустой Commit ведёт себя как "unknown" — оба приезжают из непомеченной
// сборки, разницы для Init быть не должно.
func TestResolveCommitTreatsEmptyLikeUnknown(t *testing.T) {
	exe := func() (string, error) { return "/app/server", nil }
	stat := func(string) (os.FileInfo, error) {
		return statOf{size: 1234, mod: time.Unix(1700000000, 0)}, nil
	}

	empty := resolveCommit("", exe, stat, time.Now)
	unknown := resolveCommit("unknown", exe, stat, time.Now)
	if empty != unknown {
		t.Errorf("пустой коммит (%q) и unknown (%q) дали разные отметки", empty, unknown)
	}
}

// os.Executable недоступен (редко, но бывает в чужих песочницах) — отметка
// обязана остаться уникальной, а не превратиться в постоянную строку вроде
// "unknown-boot", одинаковую при каждом старте.
func TestResolveCommitFallsBackToTimeWhenExecutableFails(t *testing.T) {
	exe := func() (string, error) { return "", errors.New("недоступно") }

	t1 := time.Unix(100, 0)
	t2 := time.Unix(200, 0)
	a := resolveCommit("unknown", exe, nil, func() time.Time { return t1 })
	b := resolveCommit("unknown", exe, nil, func() time.Time { return t2 })
	if a == b {
		t.Errorf("отметки при недоступном os.Executable совпали: %q — ошибка не даёт холодного кэша", a)
	}
}

// То же самое, если os.Stat не удался: сам путь узнать получилось, а прочитать
// его метаданные — нет.
func TestResolveCommitFallsBackToTimeWhenStatFails(t *testing.T) {
	exe := func() (string, error) { return "/app/server", nil }
	stat := func(string) (os.FileInfo, error) { return nil, errors.New("нет доступа") }

	t1 := time.Unix(100, 0)
	t2 := time.Unix(200, 0)
	a := resolveCommit("unknown", exe, stat, func() time.Time { return t1 })
	b := resolveCommit("unknown", exe, stat, func() time.Time { return t2 })
	if a == b {
		t.Errorf("отметки при неудачном os.Stat совпали: %q — ошибка не даёт холодного кэша", a)
	}
}

// Сквозная проверка публичной обёртки: настоящий os.Executable в тестовом
// процессе не падает, значит ResolveCommit("unknown") обязана дать что-то,
// отличное от буквального "unknown", — иначе Store.Init её не отличит от
// настоящей метки.
func TestResolveCommitPublicWrapperEscapesLiteralUnknown(t *testing.T) {
	got := ResolveCommit("unknown")
	if got == "unknown" || got == "" {
		t.Errorf("ResolveCommit(%q) = %q, ожидалась отметка, привязанная к файлу бинарника", "unknown", got)
	}
}
