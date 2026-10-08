package stats

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

// bots — те же семейства, что в scripts/crawler-log.py (BOTS): список шире
// карты $is_crawler в nginx, потому что знать хочется про всех, кто ходит.
// Правится в обоих местах сразу.
var bots = regexp.MustCompile(`(?i)(Googlebot|Google-InspectionTool|Storebot-Google|Google-Extended` +
	`|YandexBot|YandexRenderResourcesBot|Yandex[A-Za-z]*` +
	`|bingbot|BingPreview|DuckDuckBot|Applebot|PetalBot|SeznamBot` +
	`|Mail\.RU_Bot|MJ12bot|AhrefsBot|SemrushBot|DotBot|Bytespider` +
	`|GPTBot|OAI-SearchBot|ChatGPT-User|ClaudeBot|Claude-SearchBot|Claude-User` +
	`|PerplexityBot|Perplexity-User|Amazonbot|meta-externalagent` +
	`|TelegramBot|WhatsApp|vkShare|Twitterbot|facebookexternalhit` +
	`|Slackbot|Discordbot|LinkedInBot|SkypeUriPreview|redditbot|Mastodon)`)

// BotFamily — имя семейства бота, как оно стоит в User-Agent, или "".
func BotFamily(ua string) string {
	return bots.FindString(ua)
}

// validRefHost: публичный /api/hit принимает любой Referer, а хост уходит в
// навсегда хранимую свёртку — пускаем только то, что бывает именем хоста
// (punycode подходит).
func validRefHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

// RefHost — хост источника без www., в нижнем регистре; "" для своего хоста,
// не-http(s) схем и мусора. Сохраняется только хост: полный адрес источника
// (запрос в поисковике, личная ссылка) хранить незачем.
func RefHost(raw, ownHost string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if !validRefHost(host) {
		return ""
	}
	own := ownHost
	if h, _, err := net.SplitHostPort(ownHost); err == nil {
		own = h
	}
	if host == strings.TrimPrefix(strings.ToLower(own), "www.") {
		return ""
	}
	return host
}
