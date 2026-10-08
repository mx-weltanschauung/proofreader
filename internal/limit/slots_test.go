package limit

import (
	"context"
	"testing"
	"time"
)

func TestTryAcquireRefusesWhenFull(t *testing.T) {
	s := New(1)
	rel, ok := s.TryAcquire()
	if !ok {
		t.Fatal("первый слот не занялся")
	}
	if _, ok := s.TryAcquire(); ok {
		t.Fatal("второй слот занялся при ёмкости 1")
	}
	rel()
	if _, ok := s.TryAcquire(); !ok {
		t.Fatal("слот не освободился")
	}
}

func TestAcquireWaitsForRelease(t *testing.T) {
	s := New(1)
	rel, _ := s.TryAcquire()
	go func() {
		time.Sleep(20 * time.Millisecond)
		rel()
	}()
	rel2, ok := s.Acquire(context.Background(), time.Second)
	if !ok {
		t.Fatal("не дождался освободившегося слота")
	}
	rel2()
}

func TestAcquireTimesOut(t *testing.T) {
	s := New(1)
	s.TryAcquire()
	start := time.Now()
	if _, ok := s.Acquire(context.Background(), 30*time.Millisecond); ok {
		t.Fatal("занял слот сверх ёмкости")
	}
	if time.Since(start) < 30*time.Millisecond {
		t.Error("отказал раньше срока ожидания")
	}
}

// Ушедший клиент не оставляет занятого слота.
func TestAcquireGivesUpOnCancelAndLeaksNothing(t *testing.T) {
	s := New(1)
	rel, _ := s.TryAcquire()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool)
	go func() {
		_, ok := s.Acquire(ctx, time.Minute)
		done <- ok
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if <-done {
		t.Fatal("занял слот после отмены")
	}
	rel()
	if n := len(s); n != 0 {
		t.Fatalf("после отмены и освобождения занято %d слотов", n)
	}
}

// Повторный вызов освобождения не вынимает чужой слот.
func TestReleaseIsIdempotent(t *testing.T) {
	s := New(2)
	rel1, _ := s.TryAcquire()
	s.TryAcquire()
	rel1()
	rel1()
	if n := len(s); n != 1 {
		t.Fatalf("занято %d, ожидался 1: повторное освобождение вынуло чужой слот", n)
	}
}

func TestNestedHoldsBothAndReleasesBoth(t *testing.T) {
	outer, inner := New(1), New(2)
	rel, ok := Nested(context.Background(), outer, time.Second, inner, time.Second)
	if !ok {
		t.Fatal("не занял")
	}
	if len(outer) != 1 || len(inner) != 1 {
		t.Fatalf("занято внешних %d, внутренних %d", len(outer), len(inner))
	}
	rel()
	if len(outer) != 0 || len(inner) != 0 {
		t.Fatalf("после освобождения занято внешних %d, внутренних %d", len(outer), len(inner))
	}
}

func TestNestedReleasesOuterWhenInnerTimesOut(t *testing.T) {
	outer, inner := New(1), New(1)
	inner.TryAcquire()
	if _, ok := Nested(context.Background(), outer, time.Second, inner, 20*time.Millisecond); ok {
		t.Fatal("занял внутренний сверх ёмкости")
	}
	if n := len(outer); n != 0 {
		t.Fatalf("внешний слот остался занят (%d) после отказа внутреннего", n)
	}
}

func TestNestedDoesNotAcquireInnerWhenOuterTimesOut(t *testing.T) {
	outer, inner := New(1), New(1)
	outer.TryAcquire()
	if _, ok := Nested(context.Background(), outer, 20*time.Millisecond, inner, time.Second); ok {
		t.Fatal("занял внешний сверх ёмкости")
	}
	if n := len(inner); n != 0 {
		t.Fatalf("внутренний слот занят (%d) после отказа внешнего", n)
	}
}
