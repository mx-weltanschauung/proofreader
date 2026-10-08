package stats

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu       sync.Mutex
	rows     []Row
	salts    map[time.Time][]byte
	failSalt bool
	failIns  bool
	rollups  int
	prunes   int
}

func newFakeStore() *fakeStore { return &fakeStore{salts: map[time.Time][]byte{}} }

func (f *fakeStore) InsertViews(_ context.Context, rows []Row) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failIns {
		return errors.New("база недоступна")
	}
	f.rows = append(f.rows, rows...)
	return nil
}

func (f *fakeStore) DaySalt(_ context.Context, day time.Time) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSalt {
		return nil, errors.New("база недоступна")
	}
	if _, ok := f.salts[day]; !ok {
		f.salts[day] = []byte(day.Format("2006-01-02") + "-salt-padding-to-32b")
	}
	return f.salts[day], nil
}

func (f *fakeStore) Rollup(context.Context, time.Time) error {
	f.mu.Lock()
	f.rollups++
	f.mu.Unlock()
	return nil
}

func (f *fakeStore) Prune(context.Context, time.Time, time.Time) error {
	f.mu.Lock()
	f.prunes++
	f.mu.Unlock()
	return nil
}

func (f *fakeStore) snapshot() []Row {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Row(nil), f.rows...)
}

func (f *fakeStore) housekeepCounts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rollups, f.prunes
}

func ev(ts time.Time, ip, ua string) Event {
	return Event{Row: Row{TS: ts, Channel: ChannelSPA, Kind: "work", WorkID: 1}, IP: ip, UA: ua}
}

// runFor прогоняет рекордер до отмены и возвращает записанное.
func runFor(t *testing.T, r *Recorder, events ...Event) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	for _, e := range events {
		r.Record(e)
	}
	cancel()
	<-done
}

func TestRecorderMarkStableWithinDayAndHidesIP(t *testing.T) {
	store := newFakeStore()
	r := NewRecorder(store)
	noon := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	runFor(t, r, ev(noon, "203.0.113.4", "UA"), ev(noon.Add(time.Hour), "203.0.113.4", "UA"), ev(noon, "198.51.100.8", "UA"))
	rows := store.snapshot()
	if len(rows) != 3 {
		t.Fatalf("записано %d, ожидалось 3", len(rows))
	}
	if rows[0].Visitor == "" || rows[0].Visitor != rows[1].Visitor || rows[0].Visitor == rows[2].Visitor {
		t.Fatalf("отметки: %q %q %q", rows[0].Visitor, rows[1].Visitor, rows[2].Visitor)
	}
	if len(rows[0].Visitor) != 16 || rows[0].Visitor == "203.0.113.4" {
		t.Fatalf("отметка %q не похожа на обрезанный хэш", rows[0].Visitor)
	}
	if !rows[0].Day.Equal(time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Day = %v", rows[0].Day)
	}
}

// Review Focus 1: полночь МСК посреди одной пачки.
func TestRecorderMidnightInsideOneBatch(t *testing.T) {
	store := newFakeStore()
	r := NewRecorder(store)
	before := time.Date(2026, 9, 28, 20, 59, 58, 0, time.UTC) // 23:59:58 МСК
	after := time.Date(2026, 9, 28, 21, 0, 2, 0, time.UTC)    // 00:00:02 МСК
	runFor(t, r, ev(before, "203.0.113.4", "UA"), ev(after, "203.0.113.4", "UA"))
	rows := store.snapshot()
	if len(rows) != 2 {
		t.Fatalf("записано %d, ожидалось 2", len(rows))
	}
	if rows[0].Day.Equal(rows[1].Day) {
		t.Fatal("события по разные стороны полуночи получили одни сутки")
	}
	if rows[0].Visitor == rows[1].Visitor {
		t.Fatal("соль не сменилась на полуночи: отметки совпали")
	}
}

func TestRecorderKeepsPresetVisitorAndAgent(t *testing.T) {
	store := newFakeStore()
	r := NewRecorder(store)
	e := Event{Row: Row{TS: time.Now(), Channel: ChannelMCP, Kind: "fetch", Agent: "mcp"}}
	runFor(t, r, e)
	if got := store.snapshot()[0]; got.Visitor != "" || got.Agent != "mcp" {
		t.Fatalf("событие без адреса получило отметку: %+v", got)
	}
}

func TestRecorderDropsWhenBufferFull(t *testing.T) {
	r := newRecorder(newFakeStore(), 2) // буфер на 2, Run не запущен
	for i := 0; i < 5; i++ {
		r.Record(ev(time.Now(), "203.0.113.4", "UA")) // обязан вернуться сразу
	}
	if r.Accepted() != 2 || r.Dropped() != 3 {
		t.Fatalf("accepted=%d dropped=%d, ожидалось 2/3", r.Accepted(), r.Dropped())
	}
}

func TestRecorderNilIsNoop(t *testing.T) {
	var r *Recorder
	r.Record(ev(time.Now(), "", "")) // не паникует
}

func TestRecorderSaltFailureDropsEvent(t *testing.T) {
	store := newFakeStore()
	store.failSalt = true
	r := NewRecorder(store)
	runFor(t, r, ev(time.Now(), "203.0.113.4", "UA"))
	if len(store.snapshot()) != 0 {
		t.Fatal("событие записано без соли")
	}
	if r.WriteErrors() == 0 {
		t.Fatal("сбой соли не посчитан")
	}
}

func TestRecorderInsertFailureCounted(t *testing.T) {
	store := newFakeStore()
	store.failIns = true
	r := NewRecorder(store)
	runFor(t, r, ev(time.Now(), "203.0.113.4", "UA"))
	if r.WriteErrors() != 1 {
		t.Fatalf("write errors = %d", r.WriteErrors())
	}
}

func TestRecorderHousekeepingRuns(t *testing.T) {
	store := newFakeStore()
	r := NewRecorder(store)
	r.housekeepEvery = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	r.Run(ctx)
	if rollups, prunes := store.housekeepCounts(); rollups == 0 || prunes == 0 {
		t.Fatalf("уборка не шла: rollups=%d prunes=%d", rollups, prunes)
	}
}

// Перезапуск не откладывает удаление вчерашней соли на час: уборка идёт
// сразу при старте, а не по первому тику.
func TestRecorderHousekeepsAtStartup(t *testing.T) {
	store := newFakeStore()
	r := NewRecorder(store)
	r.housekeepEvery = time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	r.Run(ctx)
	if rollups, prunes := store.housekeepCounts(); rollups < 1 || prunes < 1 {
		t.Fatalf("уборки при старте не было: rollups=%d prunes=%d", rollups, prunes)
	}
}
