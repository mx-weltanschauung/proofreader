package storage

import (
	"context"
	"errors"
	"io"
	"regexp"
	"sync"
	"time"
)

// Reader — всё, что Fallback вправе делать с хранилищем-подстраховкой:
// читать. Методов записи и удаления у типа нет, поэтому записать или удалить
// в боевом бакете через обёртку не компилируется — защита держится типом, а
// не дисциплиной вызывающего (спека 2026-10-03-local-scans-working-set,
// часть 2).
type Reader interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	PresignGetAttachment(ctx context.Context, key string, ttl time.Duration, disposition string) (string, error)
	Head(ctx context.Context, key string) (size int64, etag string, err error)
}

// PrefixStorage — местное хранилище обёртки: полный Storage плюс вопрос
// «есть ли под префиксом хоть один объект». *S3Storage и *MemoryStorage
// подходят как есть.
type PrefixStorage interface {
	Storage
	HasPrefix(ctx context.Context, prefix string) (bool, error)
}

// prefixTTL — сколько помнится ответ HasPrefix. Записи через саму обёртку
// забывают его сразу; минута держит только записи в обход бэкенда (mc,
// restore.sh, scans-evict.sh), и там худшее — минута чтения не оттуда.
const prefixTTL = time.Minute

var workPrefixRE = regexp.MustCompile(`^works/\d+/`)

// workPrefix — префикс тома у ключа скана ("works/7/") или "" для ключа
// другой формы.
func workPrefix(key string) string { return workPrefixRE.FindString(key) }

type prefixState struct {
	local bool
	at    time.Time
}

// Fallback — местный SeaweedFS с подстраховкой боевым бакетом на чтение.
// Локально лежат только тома в работе; скан выселенного тома отдаётся из
// боевого по тому же ключу — ключ несёт work_id, а id томов локально и на
// боевом совпадают (scripts/adopt-prod-db.sh, явный id в публикаторе).
//
// Чтение решается по префиксу тома works/N/: пуст локально — сразу боевой,
// без HEAD; непуст — HEAD ключа в местном, промах — боевой. Второй случай —
// опубликованный том, которому локально подменили превью нескольких полос
// (scan_previews.py): без HEAD остальные полосы тома ушли бы в почти пустой
// местный. Лишние HEAD достаются только томам в работе. Ключ вне works/N/ —
// всегда местный. Запись и удаление — всегда местный.
type Fallback struct {
	primary   PrefixStorage
	secondary Reader
	now       func() time.Time

	mu       sync.Mutex
	prefixes map[string]prefixState
}

var _ Storage = (*Fallback)(nil)

// NewFallback собирает обёртку. secondary — только чтение (см. Reader).
func NewFallback(primary PrefixStorage, secondary Reader) *Fallback {
	return &Fallback{
		primary:   primary,
		secondary: secondary,
		now:       time.Now,
		prefixes:  map[string]prefixState{},
	}
}

func (f *Fallback) hasLocal(ctx context.Context, prefix string) (bool, error) {
	f.mu.Lock()
	st, ok := f.prefixes[prefix]
	f.mu.Unlock()
	if ok && f.now().Sub(st.at) < prefixTTL {
		return st.local, nil
	}
	local, err := f.primary.HasPrefix(ctx, prefix)
	if err != nil {
		return false, err
	}
	f.mu.Lock()
	f.prefixes[prefix] = prefixState{local: local, at: f.now()}
	f.mu.Unlock()
	return local, nil
}

// forget сбрасывает закэшированный ответ о префиксе ключа: после записи
// через обёртку «пусто» уже неправда, и ждать минуту незачем.
func (f *Fallback) forget(key string) {
	p := workPrefix(key)
	if p == "" {
		return
	}
	f.mu.Lock()
	delete(f.prefixes, p)
	f.mu.Unlock()
}

// reader выбирает, откуда читать ключ. Сбой местного хранилища — ошибка, а
// не тихий уход в боевой: иначе лежащий SeaweedFS выглядел бы исправным.
func (f *Fallback) reader(ctx context.Context, key string) (Reader, error) {
	prefix := workPrefix(key)
	if prefix == "" {
		return f.primary, nil
	}
	local, err := f.hasLocal(ctx, prefix)
	if err != nil {
		return nil, err
	}
	if !local {
		return f.secondary, nil
	}
	_, _, err = f.primary.Head(ctx, key)
	switch {
	case err == nil:
		return f.primary, nil
	case errors.Is(err, ErrObjectNotFound):
		return f.secondary, nil
	default:
		return nil, err
	}
}

func (f *Fallback) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	r, err := f.reader(ctx, key)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, key)
}

func (f *Fallback) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	r, err := f.reader(ctx, key)
	if err != nil {
		return "", err
	}
	return r.PresignGet(ctx, key, ttl)
}

func (f *Fallback) PresignGetAttachment(ctx context.Context, key string, ttl time.Duration, disposition string) (string, error) {
	r, err := f.reader(ctx, key)
	if err != nil {
		return "", err
	}
	return r.PresignGetAttachment(ctx, key, ttl, disposition)
}

func (f *Fallback) Head(ctx context.Context, key string) (int64, string, error) {
	r, err := f.reader(ctx, key)
	if err != nil {
		return 0, "", err
	}
	return r.Head(ctx, key)
}

func (f *Fallback) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	err := f.primary.Put(ctx, key, r, size, contentType)
	f.forget(key)
	return err
}

func (f *Fallback) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error) {
	f.forget(key)
	return f.primary.PresignPut(ctx, key, contentType, ttl)
}

func (f *Fallback) Delete(ctx context.Context, key string) error {
	err := f.primary.Delete(ctx, key)
	f.forget(key)
	return err
}

func (f *Fallback) DeletePrefix(ctx context.Context, prefix string) error {
	err := f.primary.DeletePrefix(ctx, prefix)
	f.mu.Lock()
	f.prefixes = map[string]prefixState{}
	f.mu.Unlock()
	return err
}
