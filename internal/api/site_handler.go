package api

import (
	"net/http"

	"proofreader/internal/config"
)

// SiteHandler отдаёт фронту то, чем этот экземпляр читальни отличается от
// любого другого (GET /api/site): имя, описание, ссылки подвала.
// Значения приходят из окружения (config.SiteConfig), образ у всех один.
type SiteHandler struct {
	site config.SiteConfig
}

func NewSiteHandler(site config.SiteConfig) *SiteHandler {
	return &SiteHandler{site: site}
}

type siteResponse struct {
	SiteName        string `json:"site_name"`
	SiteDescription string `json:"site_description"`
	SupportURL      string `json:"support_url"`
	ChannelURL      string `json:"channel_url"`
	AgeRating       string `json:"age_rating"`
	SiteTagline     string `json:"site_tagline"`
}

// Get допускает nil-получатель (обработчик не подключён — тесты роутера):
// тогда отдаётся умолчание, как при пустом окружении.
func (h *SiteHandler) Get(w http.ResponseWriter, r *http.Request) {
	var site config.SiteConfig
	if h != nil {
		site = h.site
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, siteResponse{
		SiteName:        config.SiteNameOr(site.Name),
		SiteDescription: site.Description,
		SupportURL:      site.SupportURL,
		ChannelURL:      site.ChannelURL,
		AgeRating:       site.AgeRating,
		SiteTagline:     site.Tagline,
	})
}
