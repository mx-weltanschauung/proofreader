package middleware

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// Порог сжатия. Заголовок и контрольная сумма gzip стоят около двадцати
// байт, поэтому короткий ответ после сжатия только длиннее. 1400 — размер,
// после которого ответ всё равно не уместится в один сетевой пакет, так что
// экономить нечего.
const gzipMinSize = 1400

// Уровень сжатия. Замер на живом ответе главы (8,7 МБ размеченного текста):
// уровень 6 даёт 1,42 МБ за 0,51 с, уровень 5 — 1,50 МБ за 0,27 с. Лишние
// 86 КБ едут по сети быстрее, чем сервер тратит вдвое больше времени на их
// сжатие, а ядро у него одно на всех читателей.
const gzipLevel = 5

// Жмутся только те типы, которые от этого выигрывают. Скан страницы (PNG) и
// выгрузка (архив) уже сжаты: второй проход тратит процессор впустую.
func gzipWorthIt(contentType string) bool {
	ct := contentType
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.ToLower(strings.TrimSpace(ct))

	if strings.HasPrefix(ct, "text/") {
		return true
	}
	if strings.HasSuffix(ct, "+json") || strings.HasSuffix(ct, "+xml") {
		return true
	}
	switch ct {
	case "application/json", "application/javascript", "application/xml",
		"application/x-javascript", "image/svg+xml":
		return true
	}
	return false
}

// gzipResponseWriter копит начало ответа, пока не станет ясно, стоит ли его
// жать: решение зависит и от типа содержимого (его обработчик выставляет
// уже по ходу записи), и от размера, который до конца работы обработчика
// неизвестен.
type gzipResponseWriter struct {
	http.ResponseWriter

	status  int
	buf     []byte
	decided bool
	zw      *gzip.Writer
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	// Заголовки уходят клиенту только вместе с решением о сжатии: после
	// WriteHeader добавить Content-Encoding уже нельзя.
	if w.status == 0 {
		w.status = code
	}
}

func (w *gzipResponseWriter) Write(p []byte) (int, error) {
	if w.decided {
		return w.sink().Write(p)
	}

	w.buf = append(w.buf, p...)
	if len(w.buf) < gzipMinSize {
		return len(p), nil
	}

	w.decide(true)
	if _, err := w.sink().Write(w.buf); err != nil {
		return 0, err
	}
	w.buf = nil
	return len(p), nil
}

// Flush отдаёт накопленное клиенту немедленно. Обработчик, который сбрасывает
// поток сам, ждать порога не должен — но и решение о сжатии тогда приходится
// принимать по тому, что уже накоплено.
func (w *gzipResponseWriter) Flush() {
	w.flushBuffer()
	if w.zw != nil {
		_ = w.zw.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap даёт http.ResponseController добраться до настоящего соединения.
// Встроенное поле одного этого не делает: оно промотирует Header/Write/
// WriteHeader и только их, — а без Unwrap контроллер отвечает
// ErrNotSupported, и снятие дедлайна записи тихо превращается в строку
// в журнале. Gzip стоит ближе всех к обработчику (router.go), поэтому
// обрыв цепочки именно здесь гасил дедлайн у ВСЕХ, кто его снимает:
// ввоза указателя, выгрузки и скачивания.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *gzipResponseWriter) sink() http.ResponseWriter {
	if w.zw != nil {
		return &gzipSink{ResponseWriter: w.ResponseWriter, zw: w.zw}
	}
	return w.ResponseWriter
}

// decide фиксирует выбор и отправляет заголовки. big говорит, что порог уже
// пройден: короткий ответ не жмётся независимо от типа.
func (w *gzipResponseWriter) decide(big bool) {
	if w.decided {
		return
	}
	w.decided = true

	header := w.Header()

	// Обработчик сжал ответ сам (файловый кэш глав отдаёт готовые gzip-байты
	// с диска). Второй проход превратил бы ответ в мусор: клиент разожмёт
	// один слой и получит gzip вместо JSON.
	if header.Get("Content-Encoding") != "" {
		if w.status == 0 {
			w.status = http.StatusOK
		}
		w.ResponseWriter.WriteHeader(w.status)
		return
	}

	contentType := header.Get("Content-Type")
	if contentType == "" && len(w.buf) > 0 {
		// Обработчик тип не назвал — определяем сами, как это делает
		// net/http, иначе решение принималось бы вслепую.
		contentType = http.DetectContentType(w.buf)
		header.Set("Content-Type", contentType)
	}

	if big && gzipWorthIt(contentType) {
		header.Set("Content-Encoding", "gzip")
		// Обработчик считал длину несжатого тела; оставить её — заставить
		// клиента ждать байты, которых не будет.
		header.Del("Content-Length")
		// Уровень задан константой, ошибка возможна только при неверном
		// уровне — то есть никогда; на всякий случай откатываемся к
		// умолчанию, а не роняем ответ.
		zw, err := gzip.NewWriterLevel(w.ResponseWriter, gzipLevel)
		if err != nil {
			zw = gzip.NewWriter(w.ResponseWriter)
		}
		w.zw = zw
	}

	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(w.status)
}

func (w *gzipResponseWriter) flushBuffer() {
	if !w.decided {
		w.decide(len(w.buf) >= gzipMinSize)
	}
	if len(w.buf) > 0 {
		_, _ = w.sink().Write(w.buf)
		w.buf = nil
	}
}

// close дописывает всё, что осталось в буфере, и закрывает поток gzip.
// Обязателен: без Close последний блок сжатых данных не выйдет наружу.
func (w *gzipResponseWriter) close() {
	w.flushBuffer()
	if w.zw != nil {
		_ = w.zw.Close()
	}
}

// gzipSink разводит запись тела и запись заголовков: тело идёт в поток
// сжатия, а всё остальное (Header, WriteHeader на случай повторного вызова)
// остаётся за исходным writer'ом.
type gzipSink struct {
	http.ResponseWriter
	zw *gzip.Writer
}

func (s *gzipSink) Write(p []byte) (int, error) { return s.zw.Write(p) }

// Gzip сжимает текстовые ответы, если клиент об этом просил.
//
// Чтение главы отдаёт мегабайты размеченного текста; на нём gzip выигрывает
// примерно в четыре раза, и это разница между секундами ожидания и мгновенной
// отдачей на медленном канале.
func Gzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Vary ставится всегда, даже когда сжатия не будет: без него общий
		// кэш отдал бы сжатый ответ клиенту, который gzip не просил.
		w.Header().Add("Vary", "Accept-Encoding")

		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}
