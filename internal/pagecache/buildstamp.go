package pagecache

import (
	"fmt"
	"os"
	"time"
)

// ResolveCommit достраивает отметку коммита, которую видит Store.Init.
//
// main.Commit подставляется линковщиком (`-ldflags "-X main.Commit=..."`) и
// приезжает пустым или равным "unknown" на всех путях сборки, которые не
// передают `--build-arg COMMIT` явно — это и есть `docker compose up --build`
// (умолчание в docker-compose.yml — `${COMMIT:-unknown}`), и `bootstrap.sh`
// на боевом. Отмеченным собран только `scripts/release.sh`.
//
// Без этой подстраховки такая сборка после пересборки с другим рендерером
// или другой формой ответа сверяет "unknown" с "unknown", видит совпадение и
// оставляет чужой кэш живым — обещание «изменившийся рендерер не доедет до
// читателя» держалось бы только на одном из трёх путей сборки, а не на всех.
//
// Поэтому пустой или "unknown" коммит заменяется отметкой, уникальной для
// файла бинарника: его путь, размер и время изменения. Пересобранный образ
// даёт другой файл — другой размер или mtime — и, значит, другую отметку и
// холодный кэш; перезапуск того же образа читает тот же файл и получает ту
// же отметку, и прогретый кэш переживает рестарт, как и задумано.
//
// Если сам путь к бинарнику или его stat не удались — подставляется текущее
// время: заведомо уникальное значение. Ошибиться нужно в сторону холодного
// кэша, а не горячего.
func ResolveCommit(commit string) string {
	return resolveCommit(commit, os.Executable, os.Stat, time.Now)
}

// resolveCommit — то же самое, но с внедрёнными зависимостями: тест
// подставляет свои os.Executable/os.Stat/time.Now, не трогая настоящий
// бинарник тестового процесса и не проверяя реальные часы.
func resolveCommit(
	commit string,
	executable func() (string, error),
	stat func(string) (os.FileInfo, error),
	now func() time.Time,
) string {
	if commit != "" && commit != "unknown" {
		return commit
	}

	path, err := executable()
	if err != nil {
		return fmt.Sprintf("unknown-boot-%d", now().UnixNano())
	}
	info, err := stat(path)
	if err != nil {
		return fmt.Sprintf("unknown-boot-%d", now().UnixNano())
	}
	return fmt.Sprintf("unknown-%s-%d-%d", path, info.Size(), info.ModTime().UnixNano())
}
