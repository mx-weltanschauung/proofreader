package stats

import (
	"strings"
	"testing"
)

func TestBotFamily(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)":      "Googlebot",
		"Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)":              "YandexBot",
		"TelegramBot (like TwitterBot)":                                                 "TelegramBot",
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0)": "ClaudeBot",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/129 Safari/537.36":   "",
		"Mozilla/5.0 AppleWebKit/537.36 (compatible; Perplexity-User/1.0)":              "Perplexity-User",
		"": "",
	}
	for ua, want := range cases {
		if got := BotFamily(ua); got != want {
			t.Errorf("BotFamily(%q) = %q, ожидалось %q", ua, got, want)
		}
	}
}

func TestRefHost(t *testing.T) {
	cases := []struct{ raw, own, want string }{
		{"https://yandex.ru/search/?text=ленин", "lib.example.org", "yandex.ru"},
		{"https://www.google.com/", "lib.example.org", "google.com"},
		{"https://T.ME/channel/1", "lib.example.org", "t.me"},
		{"https://lib.example.org/works/1", "lib.example.org", ""},
		{"https://lib.example.org/works/1", "lib.example.org:443", ""},
		{"not a url", "lib.example.org", ""},
		{"javascript:alert(1)", "lib.example.org", ""},
		{"", "lib.example.org", ""},
		{"https://xn--e1afmkfd.xn--p1ai/", "lib.example.org", "xn--e1afmkfd.xn--p1ai"},
		{"https://" + strings.Repeat("a", 251) + ".ru/", "lib.example.org", ""},
		{"https://пример.рф/", "lib.example.org", ""},
		{"https://ex_ample.com/", "lib.example.org", ""},
	}
	for _, c := range cases {
		if got := RefHost(c.raw, c.own); got != c.want {
			t.Errorf("RefHost(%q, %q) = %q, ожидалось %q", c.raw, c.own, got, c.want)
		}
	}
}
