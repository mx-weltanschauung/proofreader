package stats

import (
	"sync"
	"time"
)

// RateLimiter — предел событий в минуту на ключ, в памяти процесса. Точность
// не важна (окно фиксированное, рестарт его сбрасывает): задача — не пустить
// поток мусора в базу, а не отмерить честную долю.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Time
	counts map[string]int
	now    func() time.Time
}

func NewRateLimiter(perMinute int) *RateLimiter {
	return &RateLimiter{limit: perMinute, counts: map[string]int{}, now: time.Now}
}

func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.now().Truncate(time.Minute)
	if !w.Equal(l.window) {
		l.window = w
		l.counts = map[string]int{} // карта не растёт дольше минуты
	}
	if l.counts[key] >= l.limit {
		return false
	}
	l.counts[key]++
	return true
}
