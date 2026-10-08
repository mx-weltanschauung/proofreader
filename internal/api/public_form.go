package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// clientIP returns the address this request should be attributed to.
//
// На боевом цепочка «браузер → Caddy → nginx фронта → бэкенд», и RemoteAddr
// здесь — адрес контейнера nginx, один на всех: предел частоты по нему
// отрезал бы читальню целиком первым же письмом. Настоящий адрес приносит
// X-Real-IP, который проставляет nginx (frontend/nginx.conf).
//
// Верить заголовку можно только за прокси, который его перезаписывает.
// При `make run` бэкенд смотрит наружу сам, и там заголовок подделывается
// первым же curl — поэтому доверие включается явно, флагом.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
			return v
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr без порта — так бывает в тестах и за unix-сокетом.
		return strings.TrimSpace(r.RemoteAddr)
	}

	return host
}

// hashIP returns an irreversible mark of the address, or "" for an empty one.
//
// Соль обязательна: без неё все четыре миллиарда адресов перебираются за
// минуты и «хэш» не скрывает ничего. Пустой адрес даёт пустую отметку, а не
// отметку пустой строки: иначе все безадресные запросы сошлись бы под одним
// ключом и упёрлись в общий предел.
//
// IPv6 хэшируется по /64, а не целиком. Провайдеры обычно выдают абоненту
// именно /64 — это 2^64 адресов, и в пределах своей же подсети читатель
// меняет адрес свободно, обходя предел частоты сменой последних битов при
// каждой попытке. Сегодня у домена нет AAAA-записи и IPv6 до этого кода не
// доходит, но это свойство DNS, а не защиты: появится AAAA — обрезки не
// будет, и предел молча перестанет работать для IPv6-читателей.
func hashIP(ip, salt string) string {
	if ip == "" {
		return ""
	}

	key := ip
	if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() == nil {
		mask := net.CIDRMask(64, 128)
		key = parsed.Mask(mask).String()
	}

	sum := sha256.Sum256([]byte(key + salt))

	return hex.EncodeToString(sum[:16])
}

const (
	maxMessageRunes    = 4000
	maxSourcePathRunes = 500
)

// validateMessage trims the letter body and checks its length in RUNES.
// Символы, а не байты: кириллица весит по два, и len() отбил бы письмо
// вдвое короче предела, заявленного схемой (char_length).
func validateMessage(raw string) (string, error) {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return "", errors.New("сообщение не может быть пустым")
	}
	if utf8.RuneCountInString(msg) > maxMessageRunes {
		return "", fmt.Errorf("сообщение длиннее %d символов", maxMessageRunes)
	}
	// Управляющие символы (нулевой байт и прочие) Postgres в text не
	// принимает — запрос падает 500 уже после взятия блокировки предела
	// частоты, и попытка в счёт не идёт. Переводы строк и табуляция — законная
	// часть многострочного письма, их пропускаем.
	for _, r := range msg {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return "", errors.New("сообщение содержит недопустимые символы")
		}
	}

	return msg, nil
}

// normalizeSourcePath keeps only a path INSIDE the reading room.
//
// Значение приходит от отправителя и в админке становится живой ссылкой, по
// которой кликает администратор. Без проверки туда подставляется чужой сайт
// или javascript:. Всё, что не похоже на собственный путь, превращается в
// пустую строку молча: читатель в этом не виноват и отказ ему не поможет.
func normalizeSourcePath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" || utf8.RuneCountInString(path) > maxSourcePathRunes {
		return ""
	}
	// Одиночный ведущий слэш. "//host" — это адрес по схеме страницы, он
	// уводит наружу ничуть не хуже полного URL.
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return ""
	}
	// Обратный слэш браузеры местами читают как прямой: "/\host" уезжает на
	// чужой хост, выглядя как свой путь.
	if strings.ContainsRune(path, '\\') {
		return ""
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) {
			return ""
		}
	}

	return path
}
