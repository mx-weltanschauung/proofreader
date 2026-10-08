package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"proofreader/internal/limit"
)

// wait=false не ждёт нигде и ничего не оставляет занятым.
func TestHeavyAcquireNoWaitIsImmediateAndLeavesNothing(t *testing.T) {
	s := newService(Deps{BaseURL: base}, testGates())
	shared := limit.New(1)

	// Ворота заняты.
	relGate, _ := s.g.heavy.TryAcquire()
	start := time.Now()
	if _, err := s.heavyAcquire(context.Background(), shared, false); !errors.Is(err, errBusy) {
		t.Fatalf("ворота заняты: %v", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Error("ждёт у ворот")
	}
	relGate()
	if len(shared) != 0 {
		t.Error("общий слот занят при отказе у ворот")
	}

	// Общий слот занят: ворота отпущены.
	relShared, _ := shared.TryAcquire()
	start = time.Now()
	if _, err := s.heavyAcquire(context.Background(), shared, false); !errors.Is(err, errBusy) {
		t.Fatalf("общий занят: %v", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Error("ждёт общий слот")
	}
	if len(s.g.heavy) != 0 {
		t.Error("ворота остались занятыми")
	}
	relShared()

	// Свободно: берёт оба, release отпускает оба.
	rel, err := s.heavyAcquire(context.Background(), shared, false)
	if err != nil || len(s.g.heavy) != 1 || len(shared) != 1 {
		t.Fatalf("свободно: %v", err)
	}
	rel()
	if len(s.g.heavy) != 0 || len(shared) != 0 {
		t.Error("release не отпустил слоты")
	}
}

func TestHeavyAcquireWaitWaitsForFreedSlot(t *testing.T) {
	s := newService(Deps{BaseURL: base}, testGates())
	shared := limit.New(1)
	relShared, _ := shared.TryAcquire()
	go func() {
		time.Sleep(50 * time.Millisecond)
		relShared()
	}()
	rel, err := s.heavyAcquire(context.Background(), shared, true)
	if err != nil {
		t.Fatalf("wait=true не дождался: %v", err)
	}
	rel()
}

// Клиент ушёл, пока ждал ворот: это не errBusy.
func TestHeavyAcquireWaitCancelledIsContextError(t *testing.T) {
	s := newService(Deps{BaseURL: base}, testGates())
	relGate, _ := s.g.heavy.TryAcquire()
	defer relGate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.heavyAcquire(ctx, limit.New(1), true)
	if !errors.Is(err, context.Canceled) || errors.Is(err, errBusy) {
		t.Fatalf("ожидалась context.Canceled, не errBusy: %v", err)
	}
}
