package models

import "time"

// Статусы заявки на озвучку — русские строки, как статусы полос.
const (
	AudioQueued  = "в_очереди"
	AudioRunning = "синтезируется"
	AudioDone    = "готово"
	AudioFailed  = "ошибка"
)

// AudioQueueItem — заявка на озвучку главы (ChapterID != nil) или тома.
type AudioQueueItem struct {
	ID           int64          `json:"id"`
	WorkID       int64          `json:"work_id"`
	WorkTitle    string         `json:"work_title"`
	ChapterID    *int64         `json:"chapter_id"`
	ChapterTitle string         `json:"chapter_title"`
	Status       string         `json:"status"`
	Error        string         `json:"error"`
	StatusCounts map[string]int `json:"status_counts"`
	// RequestedBy — почта поставившего; пусто, если учётку удалили.
	RequestedBy string     `json:"requested_by"`
	RequestedAt time.Time  `json:"requested_at"`
	ClaimedAt   *time.Time `json:"claimed_at"`
	FinishedAt  *time.Time `json:"finished_at"`
}

// AudioTrack — синтезированная дорожка: диапазон полос тома.
type AudioTrack struct {
	ID           int64     `json:"id"`
	WorkID       int64     `json:"work_id"`
	Title        string    `json:"title"`
	StartPage    int       `json:"start_page"`
	EndPage      int       `json:"end_page"`
	S3Key        string    `json:"-"`
	DurationMS   int64     `json:"duration_ms"`
	Bytes        int64     `json:"bytes"`
	MD5          string    `json:"-"`
	RecipeSHA256 string    `json:"recipe_sha256"`
	PagesSHA256  string    `json:"pages_sha256"`
	Stale        bool      `json:"stale"`
	CreatedAt    time.Time `json:"created_at"`
}

// AudioRecording — готовая запись человека, прикреплённая к главе.
type AudioRecording struct {
	ID           int64  `json:"id"`
	WorkID       int64  `json:"work_id"`
	ChapterID    int64  `json:"chapter_id"`
	ChapterTitle string `json:"chapter_title"`
	// Диапазон главы — для порядка и отбора в плейлисте, наружу не нужен.
	ChapterStartPage int       `json:"-"`
	ChapterEndPage   int       `json:"-"`
	Position         int       `json:"position"`
	Reader           string    `json:"reader"`
	S3Key            string    `json:"-"`
	ContentType      string    `json:"content_type"`
	Bytes            int64     `json:"bytes"`
	DurationMS       int64     `json:"duration_ms"`
	UploadedBy       *int64    `json:"-"`
	CreatedAt        time.Time `json:"created_at"`
}

// AudioStaleWork — сводка устаревших дорожек по тому.
type AudioStaleWork struct {
	WorkID    int64  `json:"work_id"`
	WorkTitle string `json:"work_title"`
	Stale     int    `json:"stale"`
}

// RecordingContentTypes — разрешённые форматы записи человека и расширение
// ключа. Список повторяет CHECK в миграции 000035; иное — отказ.
var RecordingContentTypes = map[string]string{
	"audio/mpeg": "mp3",
	"audio/mp4":  "m4a",
	"audio/ogg":  "ogg",
	"audio/opus": "opus",
	"audio/flac": "flac",
}
