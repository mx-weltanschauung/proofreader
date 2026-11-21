package settings

import (
	"context"
	"sync"
	"time"
)

// Store — постоянное хранилище предела предложений.
type Store interface {
	SuggestionLimit(context.Context) (int, error)
	SetSuggestionLimit(context.Context, int) error
}

// LimitReader нужен обработчику подачи предложений.
type LimitReader interface {
	Get(context.Context) (int, error)
}

// SuggestionLimit кеширует число в памяти одного процесса.
type SuggestionLimit struct {
	store   Store
	ttl     time.Duration
	now     func() time.Time
	mu      sync.Mutex
	value   int
	expires time.Time
}

func NewSuggestionLimit(store Store, ttl time.Duration) *SuggestionLimit {
	return &SuggestionLimit{store: store, ttl: ttl, now: time.Now}
}

func (s *SuggestionLimit) Get(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.now().Before(s.expires) {
		return s.value, nil
	}
	return s.fresh(ctx)
}

// Fresh читает БД даже при действующем кеше — экран администратора видит
// актуальное значение, в том числе записанное другим процессом.
func (s *SuggestionLimit) Fresh(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fresh(ctx)
}

func (s *SuggestionLimit) fresh(ctx context.Context) (int, error) {
	started := s.now()
	value, err := s.store.SuggestionLimit(ctx)
	if err != nil {
		return 0, err
	}
	s.value = value
	s.expires = started.Add(s.ttl)
	return value, nil
}

// Set не меняет кеш, пока запись в PostgreSQL не подтверждена.
func (s *SuggestionLimit) Set(ctx context.Context, value int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	started := s.now()
	if err := s.store.SetSuggestionLimit(ctx, value); err != nil {
		return err
	}
	s.value = value
	s.expires = started.Add(s.ttl)
	return nil
}
