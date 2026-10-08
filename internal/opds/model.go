// Package opds — каталог читальни по OPDS 1.2 (Atom): собрания, тома, дерево
// глав, новые поступления и поиск для читалок и агрегаторов. Файлы каталог
// не собирает — ссылки ведут на существующие /api/.../download.
// Спека: docs/superpowers/specs/2026-10-03-opds-catalog-design.md
package opds

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// ErrNotFound — тома, узла или собрания нет в каталоге: не существует,
// служебные передние листы, аппарат, нет полос. Обработчик отвечает 404.
var ErrNotFound = errors.New("opds: не найдено")

// RequestError — отказ, который источник уже перевёл в код ответа: короткий
// поисковый запрос (400), занятые слоты поиска и истёкший таймаут (503).
// Пакет не знает, как устроен поиск, и пишет то, что ему сказали.
type RequestError struct {
	Status     int
	Message    string
	RetryAfter int // секунды; 0 — заголовка нет
}

func (e *RequestError) Error() string { return e.Message }

// Volume — том, каким его видит каталог. Печатные страницы уже со
// смещением page_offset, как в book.Meta.PageFrom/PageTo.
type Volume struct {
	ID           int64
	Title        string
	Author       string
	Lang         string // пусто — ru
	Year         int    // 0 — неизвестен
	EditionID    int64  // 0 — том вне собрания
	EditionTitle string // пусто — том вне собрания
	VolumeLabel  string // «Том 1», «Том 6, кн. 2»; пусто — без номера
	PageFrom     int
	PageTo       int
	HTMLPath     string // канонический путь читальни: /works/49-lenin-t06
	// Updated — та же метка, из которой собран ETag файла тома:
	// max(pages.updated_at, works.updated_at).
	Updated time.Time
}

// Node — глава тома с поддеревом. Отсев аппарата делает пакет (Prune), а не
// источник: признак стоит на корне поддерева и вниз не копируется.
type Node struct {
	ID          int64
	Title       string
	IsApparatus bool
	PageFrom    int // печатные, со смещением
	PageTo      int
	HTMLPath    string
	// Updated — формула CacheKey выгрузки главы: max(pages.updated_at по
	// диапазону, chapters.updated_at, works.updated_at).
	Updated time.Time
	// HasPages — в диапазоне главы есть хотя бы одна полоса. Без полос
	// выгрузка отвечает 404, и такой узел в каталог не попадает.
	HasPages bool
	Children []*Node
}

// Tree — том и его главы верхнего уровня (ещё не отсеянные).
type Tree struct {
	Volume Volume
	Nodes  []*Node
}

// Edition — собрание с томами в порядке издания.
type Edition struct {
	ID      int64
	Title   string
	Volumes []Volume
}

// Shelf — всё, из чего собираются корень, список собраний, лента собрания и
// тома вне собраний. Одним вызовом: это тот же запрос, что у /api/shelf.
type Shelf struct {
	Editions []Edition
	Loose    []Volume
}

// ChapterHit — глава, совпавшая названием. Дерево тома целиком: найдена ли
// глава в отсеянном дереве и лист ли она, решает пакет.
type ChapterHit struct {
	Tree      *Tree
	ChapterID int64
}

// VolumeHit — том с совпадениями в тексте.
type VolumeHit struct {
	Volume   Volume
	TextHits int
}

// SearchResult — выдача поиска в порядке источника.
type SearchResult struct {
	Chapters []ChapterHit
	Volumes  []VolumeHit
}

// Source — данные каталога. В приложении — api.OPDSSource.
type Source interface {
	Shelf(ctx context.Context) (*Shelf, error)
	// Recent — тома, свежие сверху, с offset; limit — сколько вернуть не
	// больше. Обработчик просит на одну запись больше, чтобы знать про next.
	Recent(ctx context.Context, limit, offset int) ([]Volume, error)
	// Tree — том с главами; ErrNotFound — нет тома, передние листы, нет полос.
	Tree(ctx context.Context, workID int64) (*Tree, error)
	// Search — поиск; *RequestError — отказ с готовым кодом. Запрос нужен
	// источнику целиком: слоты поиска ждут на его контексте, а счёт берёт
	// адрес и агента. chapters — нужны ли главы (только первой странице
	// выдачи): без них источник не строит деревьев томов.
	Search(r *http.Request, q string, chapters bool) (*SearchResult, error)
}
