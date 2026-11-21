package settings

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeLimitStore struct {
	mu          sync.Mutex
	value       int
	reads       int
	err         error
	readStarted chan struct{}
	releaseRead chan struct{}
	afterRead   func()
	afterSet    func()
}

func (s *fakeLimitStore) SuggestionLimit(context.Context) (int, error) {
	s.mu.Lock()
	value, err := s.value, s.err
	s.reads++
	started, release := s.readStarted, s.releaseRead
	afterRead := s.afterRead
	s.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		<-release
	}
	if afterRead != nil {
		afterRead()
	}
	return value, err
}

func (s *fakeLimitStore) SetSuggestionLimit(_ context.Context, value int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = value
	if s.afterSet != nil {
		s.afterSet()
	}
	return nil
}

func TestSuggestionLimitExpiryStartsBeforeDatabaseRead(t *testing.T) {
	start := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	now := start
	store := &fakeLimitStore{value: 5, afterRead: func() { now = now.Add(30 * time.Second) }}
	cache := NewSuggestionLimit(store, time.Minute)
	cache.now = func() time.Time { return now }
	if got, err := cache.Get(context.Background()); err != nil || got != 5 {
		t.Fatalf("first Get = %d, %v", got, err)
	}
	store.value = 1
	now = start.Add(time.Minute)
	if got, err := cache.Get(context.Background()); err != nil || got != 1 {
		t.Fatalf("at one minute Get = %d, %v; want fresh 1", got, err)
	}
}

func TestSuggestionLimitExpiryStartsBeforeDatabaseWrite(t *testing.T) {
	start := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	now := start
	store := &fakeLimitStore{value: 5, afterSet: func() { now = now.Add(30 * time.Second) }}
	cache := NewSuggestionLimit(store, time.Minute)
	cache.now = func() time.Time { return now }
	if err := cache.Set(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	now = start.Add(time.Minute)
	if got, err := cache.Get(context.Background()); err != nil || got != 7 || store.reads != 1 {
		t.Fatalf("at one minute Get = %d, %v, reads %d; want DB refresh", got, err, store.reads)
	}
}

func TestSuggestionLimitCachesForOneMinute(t *testing.T) {
	store := &fakeLimitStore{value: 5}
	cache := NewSuggestionLimit(store, time.Minute)
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	for _, want := range []int{5, 5} {
		got, err := cache.Get(context.Background())
		if err != nil || got != want {
			t.Fatalf("Get = %d, %v; want %d", got, err, want)
		}
	}
	if store.reads != 1 {
		t.Fatalf("reads = %d, want 1", store.reads)
	}
	store.value = 7
	now = now.Add(time.Minute)
	got, err := cache.Get(context.Background())
	if err != nil || got != 7 || store.reads != 2 {
		t.Fatalf("expired Get = %d, %v, reads %d", got, err, store.reads)
	}
}

func TestSuggestionLimitSetRefreshesCache(t *testing.T) {
	store := &fakeLimitStore{value: 5}
	cache := NewSuggestionLimit(store, time.Minute)
	if _, err := cache.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	got, err := cache.Get(context.Background())
	if err != nil || got != 7 || store.reads != 1 {
		t.Fatalf("after Set = %d, %v, reads %d", got, err, store.reads)
	}
}

func TestSuggestionLimitExpiredReadFailureDoesNotAllowSubmission(t *testing.T) {
	store := &fakeLimitStore{value: 5, err: errors.New("database unavailable")}
	cache := NewSuggestionLimit(store, time.Minute)
	if _, err := cache.Get(context.Background()); err == nil {
		t.Fatal("Get accepted absent value")
	}
	store.err = nil
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	if _, err := cache.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.err = errors.New("database unavailable")
	now = now.Add(time.Minute)
	if _, err := cache.Get(context.Background()); err == nil {
		t.Fatal("Get accepted expired value")
	}
}

func TestSuggestionLimitOldReadCannotOverwriteSet(t *testing.T) {
	store := &fakeLimitStore{value: 5, readStarted: make(chan struct{}), releaseRead: make(chan struct{})}
	cache := NewSuggestionLimit(store, time.Minute)
	readDone := make(chan struct{})
	go func() { _, _ = cache.Get(context.Background()); close(readDone) }()
	<-store.readStarted
	setDone := make(chan error, 1)
	go func() { setDone <- cache.Set(context.Background(), 7) }()
	close(store.releaseRead)
	<-readDone
	if err := <-setDone; err != nil {
		t.Fatal(err)
	}
	got, err := cache.Get(context.Background())
	if err != nil || got != 7 {
		t.Fatalf("Get after concurrent Set = %d, %v", got, err)
	}
}
