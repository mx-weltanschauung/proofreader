package stats

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"sync/atomic"
	"time"
)

const (
	bufferSize     = 10000
	batchSize      = 1000
	flushEvery     = 5 * time.Second
	housekeepEvery = time.Hour
	// RawRetention — сколько живут сырые просмотры. Свёртка — всегда.
	RawRetention = 90 * 24 * time.Hour
	// drainTimeout — сколько ждать слива буфера на остановке сервера.
	drainTimeout = 5 * time.Second
)

// Store — хранилище посещаемости; *repository.StatsRepository.
type Store interface {
	InsertViews(ctx context.Context, rows []Row) error
	DaySalt(ctx context.Context, day time.Time) ([]byte, error)
	Rollup(ctx context.Context, today time.Time) error
	Prune(ctx context.Context, rawBefore, saltBefore time.Time) error
}

// Recorder — неблокирующий буфер событий с фоновым сливом в Store. Record
// не ждёт никогда: полный буфер — отброшенное событие и счётчик. Статистика
// не имеет права задержать отдачу текста.
type Recorder struct {
	store          Store
	ch             chan Event
	housekeepEvery time.Duration
	now            func() time.Time

	accepted, dropped, writeErrors atomic.Int64

	// соль текущих суток — трогает только горутина Run
	saltDay time.Time
	salt    []byte

	lastErrLog time.Time
}

func NewRecorder(store Store) *Recorder { return newRecorder(store, bufferSize) }

func newRecorder(store Store, size int) *Recorder {
	return &Recorder{store: store, ch: make(chan Event, size), housekeepEvery: housekeepEvery, now: time.Now}
}

// Record кладёт событие в буфер. nil-получатель — ничего не делает: так
// обработчики работают и без статистики (тесты, роутер без WithStats).
func (r *Recorder) Record(e Event) {
	if r == nil {
		return
	}
	select {
	case r.ch <- e:
		r.accepted.Add(1)
	default:
		r.dropped.Add(1)
	}
}

func (r *Recorder) Accepted() int64    { return r.accepted.Load() }
func (r *Recorder) Dropped() int64     { return r.dropped.Load() }
func (r *Recorder) WriteErrors() int64 { return r.writeErrors.Load() }

// Run сливает буфер пачками и раз в housekeepEvery сворачивает сутки и
// чистит старое. На ctx.Done досливает то, что осталось, и возвращается.
func (r *Recorder) Run(ctx context.Context) {
	flush := time.NewTicker(flushEvery)
	defer flush.Stop()
	house := time.NewTicker(r.housekeepEvery)
	defer house.Stop()
	batch := make([]Event, 0, batchSize)

	// Уборка сразу при старте: иначе перезапуск в 00:30 держал бы вчерашнюю
	// соль (а с ней возможность связать заходы) ещё час, до первого тика.
	r.housekeep(ctx)

	for {
		select {
		case e := <-r.ch:
			batch = append(batch, e)
			if len(batch) >= batchSize {
				r.flush(ctx, batch)
				batch = batch[:0]
			}
		case <-flush.C:
			r.flush(ctx, batch)
			batch = batch[:0]
		case <-house.C:
			r.housekeep(ctx)
		case <-ctx.Done():
			// Родительский контекст уже отменён — слив идёт под своим.
			dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drainTimeout)
		drain:
			for {
				select {
				case e := <-r.ch:
					batch = append(batch, e)
				default:
					break drain
				}
			}
			r.flush(dctx, batch)
			cancel()
			return
		}
	}
}

func (r *Recorder) flush(ctx context.Context, batch []Event) {
	if len(batch) == 0 {
		return
	}
	rows := make([]Row, 0, len(batch))
	for _, e := range batch {
		row := e.Row
		row.Day = Day(row.TS)
		if e.IP != "" || e.UA != "" {
			v, err := r.visitor(ctx, row.Day, e.IP, e.UA)
			if err != nil {
				// Без соли отметку не посчитать, а с пустой солью она
				// обратима перебором адресов: событие теряется.
				r.fail("соль суток", err)
				continue
			}
			row.Visitor = v
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return
	}
	if err := r.store.InsertViews(ctx, rows); err != nil {
		r.fail("запись просмотров", err)
	}
}

// visitor — дневная отметка. Соль берётся по суткам события, а не по
// моменту слива: пачка бывает по обе стороны полуночи.
func (r *Recorder) visitor(ctx context.Context, day time.Time, ip, ua string) (string, error) {
	if !day.Equal(r.saltDay) || r.salt == nil {
		salt, err := r.store.DaySalt(ctx, day)
		if err != nil {
			return "", err
		}
		r.saltDay, r.salt = day, salt
	}
	h := sha256.New()
	h.Write(r.salt)
	h.Write([]byte(ip))
	h.Write([]byte{0})
	h.Write([]byte(ua))
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

func (r *Recorder) housekeep(ctx context.Context) {
	today := Day(r.now())
	if err := r.store.Rollup(ctx, today); err != nil {
		r.fail("свёртка", err)
	}
	if err := r.store.Prune(ctx, today.Add(-RawRetention), today); err != nil {
		r.fail("чистка", err)
	}
}

// fail считает ошибку и пишет в журнал не чаще раза в минуту.
func (r *Recorder) fail(what string, err error) {
	r.writeErrors.Add(1)
	if now := r.now(); now.Sub(r.lastErrLog) >= time.Minute {
		r.lastErrLog = now
		log.Printf("статистика: %s: %v", what, err)
	}
}
