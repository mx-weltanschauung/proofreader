package seo

import "embed"

// Fonts — шрифт Literata с лицензией, тот же, что рисует карточки превью.
// Его же кладёт в assets/fonts статическая читальня (internal/staticsite):
// второй копии файлов шрифта в репозитории не заводим.
//
//go:embed assets/Literata-Regular.ttf assets/Literata-Bold.ttf assets/LICENSE-Literata.txt
var Fonts embed.FS
