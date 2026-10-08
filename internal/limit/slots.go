// Package limit — ограничители одновременности: сколько тяжёлых запросов идёт
// разом. Канал с ёмкостью, как было в поиске и в /seo, вынесенный в тип, чтобы
// один и тот же ограничитель делили сайт и MCP-сервер читальни
// (docs/superpowers/specs/2026-09-29-corpus-mcp-design.md, «Ограничители»).
package limit

import (
	"context"
	"sync"
	"time"
)

// Slots — ограничитель на cap(s) одновременных работ. Именованный канал, а не
// структура: тесты соседних пакетов занимают слоты прямой записью
// (h.heavy <- struct{}{}), и так это работать и продолжает.
type Slots chan struct{}

// New — ограничитель на n одновременных работ.
func New(n int) Slots { return make(Slots, n) }

// release — освобождение, безопасное к повторному вызову: defer и явный вызов
// в одной ветке иначе вынули бы чужой слот.
func (s Slots) release() func() {
	var once sync.Once
	return func() { once.Do(func() { <-s }) }
}

// TryAcquire — занять слот без ожидания.
func (s Slots) TryAcquire() (func(), bool) {
	select {
	case s <- struct{}{}:
		return s.release(), true
	default:
		return nil, false
	}
}

// Acquire — занять слот, ожидая не дольше wait и не дольше жизни ctx: держать
// очередь на запрос, который уже некому прочитать, — это слот, отобранный у
// того, кто ещё ждёт.
func (s Slots) Acquire(ctx context.Context, wait time.Duration) (func(), bool) {
	if rel, ok := s.TryAcquire(); ok {
		return rel, true
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case s <- struct{}{}:
		return s.release(), true
	case <-timer.C:
		return nil, false
	case <-ctx.Done():
		return nil, false
	}
}

// Nested — сперва внешний слот (свои ворота), потом внутренний (общий с
// сайтом). Порядок всегда один и тот же, поэтому взаимной блокировки нет.
// Внутренний не дождались — внешний отпускается: держать ворота без работы
// значит задерживать очередь впустую.
func Nested(ctx context.Context, outer Slots, outerWait time.Duration, inner Slots, innerWait time.Duration) (func(), bool) {
	relOuter, ok := outer.Acquire(ctx, outerWait)
	if !ok {
		return nil, false
	}
	relInner, ok := inner.Acquire(ctx, innerWait)
	if !ok {
		relOuter()
		return nil, false
	}
	return func() {
		relInner()
		relOuter()
	}, true
}
