package models

// EventOrganization — организация, проводящая события (съезды, пленумы).
type EventOrganization struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Position    int    `json:"position"`
}

// EventKind — вид событий внутри организации («Съезды», «Пленумы»).
type EventKind struct {
	ID             int64  `json:"id"`
	OrganizationID int64  `json:"organization_id"`
	Title          string `json:"title"`
	Position       int    `json:"position"`
}

// Event — одно событие (съезд, пленум).
type Event struct {
	ID          int64  `json:"id"`
	KindID      int64  `json:"kind_id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Dates       string `json:"dates"`
	Place       string `json:"place"`
	Description string `json:"description"`
	Position    int    `json:"position"`
}

// Виды цели строки повестки.
const (
	EventTargetChapter = "chapter"
	EventTargetWork    = "work"
)

// EventTarget — цель строки: глава или том. Title — живое заглавие, у битой
// цели — снимок. Broken: работа исчезла, либо цель-глава потеряла главу.
// Closed заполняет обработчик и только для сотрудника.
type EventTarget struct {
	Kind      string `json:"kind"`
	WorkID    *int64 `json:"work_id"`
	ChapterID *int64 `json:"chapter_id"`
	Label     string `json:"label"`
	Title     string `json:"title"`
	WorkTitle string `json:"work_title"`
	Broken    bool   `json:"broken"`
	Closed    bool   `json:"closed"`
}

// EventEntry — строка пункта повестки («Доклад …») с целями.
type EventEntry struct {
	Label   string        `json:"label"`
	Targets []EventTarget `json:"targets"`
}

// EventSection — пункт повестки. Position — хранимая позиция с нуля: якорь
// пункта на странице не сдвигается, когда ранние пункты скрыты от читателя.
type EventSection struct {
	Position int          `json:"position"`
	Title    string       `json:"title"`
	Entries  []EventEntry `json:"entries"`
}

// EventDetail — событие целиком.
type EventDetail struct {
	Event
	Organization EventOrganization `json:"organization"`
	KindTitle    string            `json:"kind_title"`
	Sections     []EventSection    `json:"sections"`
}

// EventSummary — строка события в списке организации.
// TargetWorkIDs — work_id каждой небитой цели, с повторами; наружу не идёт.
type EventSummary struct {
	ID             int64   `json:"id"`
	Slug           string  `json:"slug"`
	Title          string  `json:"title"`
	Dates          string  `json:"dates"`
	MaterialsTotal int     `json:"materials_total"`
	TargetWorkIDs  []int64 `json:"-"`
	// TargetChapterIDs — chapter_id целей, по порядку TargetWorkIDs; 0 — цель
	// целая работа. Наружу не идёт.
	TargetChapterIDs []int64 `json:"-"`
}

// EventKindEvents — вид со своими событиями.
type EventKindEvents struct {
	EventKind
	Events []EventSummary `json:"events"`
}

// EventOrganizationDetail — организация с видами и событиями.
type EventOrganizationDetail struct {
	EventOrganization
	Kinds []EventKindEvents `json:"kinds"`
}

// ChapterEventLink — «Входит в: …» у главы. SectionPosition — с нуля.
type ChapterEventLink struct {
	OrganizationSlug  string `json:"organization_slug"`
	OrganizationTitle string `json:"organization_title"`
	EventSlug         string `json:"event_slug"`
	EventTitle        string `json:"event_title"`
	SectionTitle      string `json:"section_title"`
	SectionPosition   int    `json:"section_position"`
}

// Тело записи повестки (PUT /events/{id}/agenda).
type AgendaInput struct {
	Sections []AgendaSectionInput `json:"sections"`
}

type AgendaSectionInput struct {
	Title   string             `json:"title"`
	Entries []AgendaEntryInput `json:"entries"`
}

type AgendaEntryInput struct {
	Label   string              `json:"label"`
	Targets []AgendaTargetInput `json:"targets"`
}

type AgendaTargetInput struct {
	Kind      string `json:"kind"`
	WorkID    int64  `json:"work_id"`
	ChapterID *int64 `json:"chapter_id"`
	Label     string `json:"label"`
}
