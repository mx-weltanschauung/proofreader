package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientIPIgnoresProxyHeaderWhenUntrusted(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/feedback", strings.NewReader("{}"))
	req.RemoteAddr = "10.0.0.7:53124"
	req.Header.Set("X-Real-IP", "203.0.113.4")

	// Без заслонки любой клиент подделывает заголовок и обходит предел
	// частоты, не прилагая усилий.
	if got := clientIP(req, false); got != "10.0.0.7" {
		t.Errorf("clientIP(untrusted) = %q, ожидалось 10.0.0.7", got)
	}
	if got := clientIP(req, true); got != "203.0.113.4" {
		t.Errorf("clientIP(trusted) = %q, ожидалось 203.0.113.4", got)
	}
}

func TestClientIPFallsBackWhenHeaderEmpty(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/feedback", strings.NewReader("{}"))
	req.RemoteAddr = "10.0.0.7:53124"
	req.Header.Set("X-Real-IP", "   ")

	if got := clientIP(req, true); got != "10.0.0.7" {
		t.Errorf("clientIP = %q, ожидалось 10.0.0.7 (пустой заголовок не в счёт)", got)
	}
}

func TestClientIPHandlesAddrWithoutPort(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/feedback", strings.NewReader("{}"))
	req.RemoteAddr = "10.0.0.7"

	if got := clientIP(req, false); got != "10.0.0.7" {
		t.Errorf("clientIP = %q, ожидалось 10.0.0.7", got)
	}
}

func TestHashIPIsSaltedAndIrreversible(t *testing.T) {
	a := hashIP("203.0.113.4", "соль-один")
	b := hashIP("203.0.113.4", "соль-два")

	if a == b {
		t.Error("соль не участвует в отметке: без неё адреса перебираются за минуты")
	}
	if len(a) != 32 {
		t.Errorf("длина отметки %d, ожидалось 32 (16 байт в hex)", len(a))
	}
	if strings.Contains(a, "203.0.113.4") {
		t.Error("адрес виден в отметке")
	}
	if hashIP("203.0.113.4", "соль-один") != a {
		t.Error("отметка должна быть устойчивой: иначе счётчик частоты не сходится")
	}
	// Пустой адрес не превращается в общую отметку, под которой сойдутся все
	// безадресные запросы и упрутся в общий предел.
	if hashIP("", "соль-один") != "" {
		t.Error("пустой адрес обязан давать пустую отметку")
	}
}

func TestHashIPCollapsesIPv6ToSlash64(t *testing.T) {
	salt := "соль"

	// Провайдер обычно выдаёт абоненту /64 целиком: два адреса в одном /64
	// обязаны давать одну отметку, иначе предел частоты обходится сменой
	// последних битов на каждую попытку.
	a := hashIP("2001:db8:1234:5678::1", salt)
	b := hashIP("2001:db8:1234:5678:ffff:ffff:ffff:ffff", salt)
	if a != b {
		t.Errorf("адреса одного /64 дали разные отметки: %q != %q", a, b)
	}

	// Другая /64-подсеть — другая отметка.
	c := hashIP("2001:db8:1234:5679::1", salt)
	if a == c {
		t.Error("адреса разных /64 дали одну отметку")
	}

	// IPv4 обрезка не касается: два разных адреса остаются разными.
	v4a := hashIP("203.0.113.4", salt)
	v4b := hashIP("203.0.113.5", salt)
	if v4a == v4b {
		t.Error("разные IPv4-адреса дали одну отметку")
	}
}

func TestValidateMessage(t *testing.T) {
	got, err := validateMessage("  нашёл опечатку  ")
	if err != nil {
		t.Fatalf("validateMessage: %v", err)
	}
	if got != "нашёл опечатку" {
		t.Errorf("обрамляющие пробелы должны срезаться, получено %q", got)
	}

	if _, err := validateMessage("   \n\t "); err == nil {
		t.Error("сообщение из одних пробелов обязано отбиваться")
	}

	// 4000 кириллических символов — 8000 байт. Считать надо символы: с len()
	// это письмо отбилось бы, хотя оно ровно в пределе.
	long := strings.Repeat("я", maxMessageRunes)
	if _, err := validateMessage(long); err != nil {
		t.Errorf("письмо ровно в предел отбито: %v", err)
	}
	if _, err := validateMessage(long + "я"); err == nil {
		t.Error("письмо сверх предела обязано отбиваться")
	}
}

func TestValidateMessageRejectsControlBytesButKeepsNewlines(t *testing.T) {
	// Нулевой байт Postgres в text не принимает: запрос падал 500 уже после
	// взятия блокировки предела частоты, и попытка не шла в счёт.
	if _, err := validateMessage("нашёл опечатку\x00 на странице"); err == nil {
		t.Error("сообщение с нулевым байтом обязано отбиваться")
	}

	// Письмо многострочное: переводы строк и табуляция — законный текст, не
	// управляющий мусор.
	got, err := validateMessage("первая строка\nвторая\tстрока\r\n")
	if err != nil {
		t.Errorf("перевод строки и табуляция обязаны проходить: %v", err)
	}
	if got != "первая строка\nвторая\tстрока" {
		t.Errorf("неожиданный результат: %q", got)
	}
}

func TestNormalizeSourcePath(t *testing.T) {
	ok := []string{
		"/works/16/pages/412",
		"/concepts/materializm?letter=М",
		"/",
	}
	for _, p := range ok {
		if got := normalizeSourcePath(p); got != p {
			t.Errorf("normalizeSourcePath(%q) = %q, ожидался тот же путь", p, got)
		}
	}

	// Значение приходит от отправителя, а в админке становится ссылкой, по
	// которой кликает администратор. Всё, что уводит наружу, обязано стать
	// пустой строкой — молча, без ошибки: читатель тут ни при чём.
	bad := []string{
		"https://чужой.сайт/страница",
		"//чужой.сайт/страница",
		`/\чужой.сайт`,
		"javascript:alert(1)",
		"works/16",
		"/works/16\nSet-Cookie: a=b",
		strings.Repeat("/я", maxSourcePathRunes),
	}
	for _, p := range bad {
		if got := normalizeSourcePath(p); got != "" {
			t.Errorf("normalizeSourcePath(%q) = %q, ожидалась пустая строка", p, got)
		}
	}

	// Граница включительно: путь ровно в 500 символов обязан проходить.
	exact := "/" + strings.Repeat("a", maxSourcePathRunes-1)
	if got := normalizeSourcePath(exact); got != exact {
		t.Errorf("normalizeSourcePath(путь ровно в предел) = %q, ожидался тот же путь", got)
	}
}
