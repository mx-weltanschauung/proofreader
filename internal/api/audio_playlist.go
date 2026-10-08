package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"proofreader/internal/audio"
	"proofreader/pkg/book"
)

// AudioPlaylist собирает .m3u тома или главы: записи человека и синтез,
// абсолютными адресами от PUBLIC_BASE_URL — плеер открывает файл вне сайта.
type AudioPlaylist struct {
	tracks   AudioTrackStore
	recs     AudioRecordingStore
	works    WorkStore
	chapters ChapterGetter
	baseURL  string
}

func NewAudioPlaylist(tracks AudioTrackStore, recs AudioRecordingStore, works WorkStore,
	chapters ChapterGetter, baseURL string) *AudioPlaylist {
	return &AudioPlaylist{tracks: tracks, recs: recs, works: works, chapters: chapters, baseURL: baseURL}
}

// serve отдаёт плейлист; chapterID == 0 — весь том.
func (p *AudioPlaylist) serve(ctx context.Context, w http.ResponseWriter, workID, chapterID int64) {
	title, start, end := "", 0, 0
	if chapterID != 0 {
		c, err := p.chapters.GetByID(ctx, chapterID)
		if err != nil || c == nil || c.WorkID != workID {
			http.Error(w, "Не найдено", http.StatusNotFound)
			return
		}
		title, start, end = c.Title, c.StartPage, c.EndPage
	} else {
		work, err := p.works.GetByID(ctx, workID)
		if err != nil || work == nil {
			http.Error(w, "Не найдено", http.StatusNotFound)
			return
		}
		title = work.Title
	}
	tracks, err := p.tracks.ListTracks(ctx, workID)
	if err != nil {
		http.Error(w, "Не удалось собрать плейлист", http.StatusInternalServerError)
		return
	}
	recs, err := p.recs.ListRecordings(ctx, workID)
	if err != nil {
		http.Error(w, "Не удалось собрать плейлист", http.StatusInternalServerError)
		return
	}
	entries := audio.Playlist(tracks, recs, start, end, p.baseURL)
	if len(entries) == 0 {
		what := "тома"
		if chapterID != 0 {
			what = "главы"
		}
		http.Error(w, fmt.Sprintf("У этого %s пока нет звука", what), http.StatusNotFound)
		return
	}
	var buf bytes.Buffer
	_ = audio.WriteM3U(&buf, entries)
	w.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
	ascii := fmt.Sprintf("work-%d", workID)
	if chapterID != 0 {
		ascii = fmt.Sprintf("chapter-%d", chapterID)
	}
	w.Header().Set("Content-Disposition", book.ContentDisposition(ascii, title, "m3u"))
	_, _ = w.Write(buf.Bytes())
}
