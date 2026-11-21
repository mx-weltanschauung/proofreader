package api

import (
	"encoding/json"
	"io"
	"net/http"

	"proofreader/internal/stats"
)

type visiblePageBody struct {
	WorkID     int64 `json:"work_id"`
	PageNumber int   `json:"page_number"`
}

// VisiblePage принимает неблокирующую отметку полосы. Ответ одинаков и для
// принятых событий, и для отсеянного мусора.
func (h *StatsHandler) VisiblePage(w http.ResponseWriter, r *http.Request) {
	defer w.WriteHeader(http.StatusNoContent)
	if h == nil || h.rec == nil {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxHitBody+1))
	if err != nil || len(raw) > maxHitBody {
		return
	}
	var body visiblePageBody
	if json.Unmarshal(raw, &body) != nil || body.WorkID < 1 || body.WorkID > 1_000_000_000 ||
		body.PageNumber < 1 || body.PageNumber > 1_000_000 {
		return
	}
	ua := r.UserAgent()
	if stats.BotFamily(ua) != "" || isStaffRequest(r) {
		return
	}
	ip := clientIP(r, h.trustProxy)
	if !h.limiter.Allow(limiterKey(ip)) || !h.global.Allow("all") {
		return
	}
	h.rec.Record(stats.Event{Row: stats.Row{TS: h.now(), Channel: stats.ChannelReading,
		WorkID: body.WorkID, EntityID: int64(body.PageNumber)}, IP: ip, UA: ua})
}
