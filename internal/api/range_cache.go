package api

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"proofreader/internal/pagecache"
)

// RangeCache отдаёт ответы, адресуемые диапазоном полос: главы и элементы
// подборок. Оба обработчика строят ответ ровно из тройки (work_id, start,
// end) и потому делят один файл.
//
// Здесь же живёт единый полёт: без него ссылка на свежую главу, разосланная
// нескольким читателям сразу, запускает столько же одинаковых рендеров.
type RangeCache struct {
	store *pagecache.Store

	mu      sync.Mutex
	flights map[pagecache.Key]*rangeFlight
}

// rangeFlight — сборка, которую уже кто-то делает.
type rangeFlight struct {
	done chan struct{}
	raw  []byte
	err  error
	// waiters — сколько запросов ждут этой сборки вместо своей. Считается
	// под c.mu; по нему тест детерминированно видит, что склейка случилась,
	// не полагаясь на сон.
	waiters int
}

func NewRangeCache(store *pagecache.Store) *RangeCache {
	return &RangeCache{
		store:   store,
		flights: map[pagecache.Key]*rangeFlight{},
	}
}

// Serve отдаёт готовое тело из кэша, а если его нет — собирает через build,
// кладёт в кэш и отдаёт.
//
// nil-приёмник допустим и означает «кэша нет»: так собран обработчик в
// части тестов, и ронять их из-за кэша незачем.
func (c *RangeCache) Serve(
	w http.ResponseWriter,
	r *http.Request,
	key pagecache.Key,
	build func() (any, error),
) {
	if c.serveFromFile(w, r, key) {
		return
	}

	raw, err := c.buildOnce(r, key, build)
	if err != nil {
		if errors.Is(err, errClientGone) {
			return
		}
		log.Printf("сборка диапазона %+v: %v", key, err)
		writeError(w, http.StatusInternalServerError, "Не удалось собрать страницы")
		return
	}

	// Несжатыми: сжатие возьмёт на себя middleware.Gzip — ровно как сегодня,
	// без кэша. Первый читатель платит столько же, сколько платил бы всегда.
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(raw); err != nil {
		log.Printf("отдача диапазона %+v прервана: %v", key, err)
	}
}

// serveFromFile отдаёт готовые байты. Возвращает false, если отдать нечего
// и надо собирать.
func (c *RangeCache) serveFromFile(w http.ResponseWriter, r *http.Request, key pagecache.Key) bool {
	if c == nil {
		return false
	}
	rc, size, ok := c.store.Open(key)
	if !ok {
		return false
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "application/json")

	if acceptsGzip(r) {
		// Байты уезжают как есть — ни разжатия, ни повторного сжатия.
		// middleware.Gzip видит выставленный Content-Encoding и не трогает
		// тело (см. его decide).
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		if _, err := io.Copy(w, rc); err != nil {
			log.Printf("отдача кэша %+v прервана: %v", key, err)
		}
		return true
	}

	zr, err := gzip.NewReader(rc)
	if err != nil {
		// Битый файл (обрыв питания между Rename и сбросом ФС) — сносим и
		// собираем заново, читатель ничего не замечает.
		log.Printf("битый файл кэша %+v: %v", key, err)
		if _, delErr := c.store.Delete(key); delErr != nil {
			log.Printf("не удалось снести битый файл %+v: %v", key, delErr)
		}
		return false
	}
	defer zr.Close()

	if _, err := io.Copy(w, zr); err != nil {
		log.Printf("отдача кэша %+v прервана: %v", key, err)
	}
	return true
}

// errClientGone — читатель ушёл, пока ждал чужой сборки. Не ошибка сервера
// и в журнал не пишется.
var errClientGone = errors.New("читатель отсоединился")

// buildOnce собирает тело, следя, чтобы одну и ту же работу не делали дважды.
func (c *RangeCache) buildOnce(
	r *http.Request,
	key pagecache.Key,
	build func() (any, error),
) ([]byte, error) {
	if c == nil {
		return marshalBuild(build)
	}

	c.mu.Lock()
	if fl, ok := c.flights[key]; ok {
		fl.waiters++
		c.mu.Unlock()
		select {
		case <-fl.done:
		case <-r.Context().Done():
			return nil, errClientGone
		}
		if fl.raw == nil && fl.err == nil {
			// Ведущий запаниковал: defer закрыл done, но полей не выставил.
			// Без этой проверки ждущий отдал бы пустое тело как успех.
			return nil, errors.New("сборка не состоялась")
		}
		return fl.raw, fl.err
	}

	fl := &rangeFlight{done: make(chan struct{})}
	c.flights[key] = fl
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.flights, key)
		c.mu.Unlock()
		close(fl.done)
	}()

	raw, err := marshalBuild(build)
	if err != nil {
		fl.err = err
		return nil, err
	}

	// Беда кэша не имеет права стать ошибкой читателю: тело уже собрано.
	if err := c.store.Put(key, raw); err != nil {
		log.Printf("не удалось записать кэш %+v: %v", key, err)
	}

	fl.raw = raw
	return raw, nil
}

// waiterCount — сколько запросов сейчас ждут чужой сборки этого ключа.
func (c *RangeCache) waiterCount(key pagecache.Key) int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if fl, ok := c.flights[key]; ok {
		return fl.waiters
	}
	return 0
}

func marshalBuild(build func() (any, error)) ([]byte, error) {
	v, err := build()
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func acceptsGzip(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept-Encoding")), "gzip")
}
