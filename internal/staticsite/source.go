package staticsite

import (
	"context"

	"proofreader/internal/models"
	"proofreader/pkg/book"
)

// Source — всё, что генератору нужно от читальни. Объявлен здесь, у
// потребителя: генератор проверяется на подставном источнике без базы.
// Настоящий — *api.StaticSource (internal/api/static_source.go); импортировать
// internal/api сюда нельзя — он сам импортирует этот пакет.
type Source interface {
	// Shelf — полки главной: издания с томами и тома вне изданий.
	Shelf(ctx context.Context) (*models.Shelf, error)
	// Highlights — избранное всех изданий.
	Highlights(ctx context.Context) ([]models.EditionHighlight, error)
	// Work — работа по id: у томов вне изданий на полке только заглавие.
	Work(ctx context.Context, id int64) (*models.Work, error)
	// FrontMatter — служебные передние листы тома.
	FrontMatter(ctx context.Context, volumeID int64) ([]*models.Work, error)
	// Volume — работа книгой: секции верхнего уровня (главы с ChapterID и
	// безымянные хвосты), якоря сносок уже не пересекаются между секциями.
	// nil без ошибки — у работы нет ни одной полосы.
	Volume(ctx context.Context, workID int64) (*book.Book, error)
	// ConceptList — понятия-статьи указателя по алфавиту.
	ConceptList(ctx context.Context) ([]ConceptEntry, error)
	Concept(ctx context.Context, slug string) (*Concept, error)
	// Collections — только опубликованные сотруднические подборки.
	Collections(ctx context.Context) ([]Collection, error)
	// Documents — только опубликованные разборы, в одобренной редакции.
	Documents(ctx context.Context) ([]Document, error)
}

type ConceptEntry struct {
	Slug, Title string
	Places      int
}

type Concept struct {
	Title    string
	Articles []ConceptArticle
}

// ConceptArticle — статья одного указателя о понятии.
type ConceptArticle struct {
	Edition string
	// WorkID — том, из указателя которого статья (index_concept_articles.
	// work_id); 0 — не известен. Статья идёт в архив вместе с этим томом.
	WorkID int64
	// HTML — текст статьи, уже отрендеренный; пусто — текста нет (у
	// ленинского указателя только адреса).
	HTML  string
	Refs  []ConceptRef
	Links []ConceptLink
}

// ConceptRef — один адрес указателя.
type ConceptRef struct {
	// Path — путь подрубрики от корня статьи; пусто — адрес висит на статье.
	Path []string
	// Label — «т. 33, с. 120—121».
	Label string
	// WorkID и Page — полоса (внутренний номер), в которую адрес
	// разрешился; WorkID == 0 — не разрешился (тома нет в читальне).
	WorkID    int64
	Page      int
	Uncertain bool
	Note      string
}

// ConceptLink — «см.» или «см. также». Slug пуст — цели в каталоге нет.
type ConceptLink struct {
	Kind, Title, Slug string
}

type Collection struct {
	ID                       int64
	Slug, Title, Description string
	Items                    []CollectionItem
}

// CollectionItem — пункт подборки: глава (ChapterID != 0) или том целиком.
type CollectionItem struct {
	Title, Author string
	WorkID        int64
	ChapterID     int64
	// StartPage — первая полоса главы (внутренний номер): запасной путь,
	// если сама глава в сборке своего якоря не получила.
	StartPage int
	// Broken — источник удалён (глава или том), пункт печатается без ссылки.
	Broken bool
}

type Document struct {
	ID          int64
	Slug, Title string
	// Author — ник читателя; пусто — сотруднический разбор.
	Author   string
	BodyHTML string
}
