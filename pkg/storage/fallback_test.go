package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fbLocal — местное хранилище обёртки: считает вопросы о префиксе и умеет
// отказать, чтобы было видно, что сбой местного не прячется за боевым.
type fbLocal struct {
	*MemoryStorage
	asked int
	err   error
}

func (l *fbLocal) HasPrefix(ctx context.Context, prefix string) (bool, error) {
	l.asked++
	if l.err != nil {
		return false, l.err
	}
	return l.MemoryStorage.HasPrefix(ctx, prefix)
}

// fbProd — боевое хранилище: подписанные ссылки помечены, иначе ссылку
// местного MemoryStorage от боевого не отличить.
type fbProd struct{ *MemoryStorage }

func (p fbProd) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := p.MemoryStorage.PresignGet(ctx, key, ttl)
	return "prod:" + u, err
}

func (p fbProd) PresignGetAttachment(ctx context.Context, key string, ttl time.Duration, d string) (string, error) {
	u, err := p.MemoryStorage.PresignGetAttachment(ctx, key, ttl, d)
	return "prod:" + u, err
}

func fbPut(t *testing.T, s Storage, key, body string) {
	t.Helper()
	if err := s.Put(context.Background(), key, strings.NewReader(body), int64(len(body)), "image/png"); err != nil {
		t.Fatalf("Put(%s): %v", key, err)
	}
}

func fbRead(t *testing.T, f *Fallback, key string) string {
	t.Helper()
	rc, err := f.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get(%s): %v", key, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll(%s): %v", key, err)
	}
	return string(b)
}

func fbPair() (*fbLocal, fbProd, *Fallback) {
	local := &fbLocal{MemoryStorage: NewMemoryStorage()}
	prod := fbProd{NewMemoryStorage()}
	return local, prod, NewFallback(local, prod)
}

// Выселенный том: локально под works/7/ пусто — и Get, и подписанная ссылка
// уходят в боевой, без HEAD в местном.
func TestFallbackReadsEvictedVolumeFromSecondary(t *testing.T) {
	_, prod, f := fbPair()
	fbPut(t, prod, "works/7/pages/page_1.png", "prod")

	if got := fbRead(t, f, "works/7/pages/page_1.png"); got != "prod" {
		t.Fatalf("Get = %q, ждали скан из боевого", got)
	}
	u, err := f.PresignGet(context.Background(), "works/7/pages/page_1.png", time.Minute)
	if err != nil || !strings.HasPrefix(u, "prod:") {
		t.Fatalf("PresignGet = %q, %v; ждали ссылку боевого", u, err)
	}
	u, err = f.PresignGetAttachment(context.Background(), "works/7/original/x.pdf", time.Minute, "attachment")
	if err != nil || !strings.HasPrefix(u, "prod:") {
		t.Fatalf("PresignGetAttachment = %q, %v; ждали ссылку боевого", u, err)
	}
}

// Том в работе лежит локально целиком — читается местный, даже если в
// боевом под тем же ключом что-то есть.
func TestFallbackReadsLocalVolumeFromPrimary(t *testing.T) {
	local, prod, f := fbPair()
	fbPut(t, local, "works/7/pages/page_1.png", "local")
	fbPut(t, prod, "works/7/pages/page_1.png", "prod")

	if got := fbRead(t, f, "works/7/pages/page_1.png"); got != "local" {
		t.Fatalf("Get = %q, ждали местный скан", got)
	}
	u, err := f.PresignGet(context.Background(), "works/7/pages/page_1.png", time.Minute)
	if err != nil || strings.HasPrefix(u, "prod:") {
		t.Fatalf("PresignGet = %q, %v; ждали местную ссылку", u, err)
	}
}

// scan_previews.py подменяет превью нескольких полос опубликованного тома:
// подменённые читаются локально, остальные — из боевого, по ключу.
func TestFallbackPartiallyLocalVolumeResolvesPerKey(t *testing.T) {
	local, prod, f := fbPair()
	fbPut(t, local, "works/7/pages/page_1.png", "local-1")
	fbPut(t, prod, "works/7/pages/page_1.png", "prod-1")
	fbPut(t, prod, "works/7/pages/page_2.png", "prod-2!")

	if got := fbRead(t, f, "works/7/pages/page_1.png"); got != "local-1" {
		t.Fatalf("page_1 = %q, ждали местную подмену", got)
	}
	if got := fbRead(t, f, "works/7/pages/page_2.png"); got != "prod-2!" {
		t.Fatalf("page_2 = %q, ждали боевую полосу", got)
	}
	size, _, err := f.Head(context.Background(), "works/7/pages/page_2.png")
	if err != nil || size != int64(len("prod-2!")) {
		t.Fatalf("Head(page_2) = %d, %v; ждали размер боевого объекта", size, err)
	}
}

// Ключ вне works/N/ — всегда местный; о префиксе обёртка и не спрашивает.
func TestFallbackKeyOutsideWorksGoesToPrimary(t *testing.T) {
	local, prod, f := fbPair()
	fbPut(t, prod, "other/x.png", "prod")

	if _, err := f.Get(context.Background(), "other/x.png"); err == nil {
		t.Fatal("ключ вне works/N/ прочитан из боевого")
	}
	if local.asked != 0 {
		t.Fatalf("обёртка спрашивала о префиксе %d раз, ключ вне works/N/", local.asked)
	}
}

// Запись и удаление идят только в местное: боевой бакет обёртка не трогает
// никогда (secondary объявлен Reader — писать в него нечем и при желании).
func TestFallbackWritesNeverReachSecondary(t *testing.T) {
	_, prod, f := fbPair()
	ctx := context.Background()
	fbPut(t, prod, "works/7/pages/page_1.png", "prod")

	if err := f.Delete(ctx, "works/7/pages/page_1.png"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := f.DeletePrefix(ctx, "works/7/"); err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}
	fbPut(t, f, "works/8/pages/page_1.png", "new")
	if _, err := f.PresignPut(ctx, "works/8/pages/page_2.png", "image/png", time.Minute); err != nil {
		t.Fatalf("PresignPut: %v", err)
	}

	if _, _, err := prod.Head(ctx, "works/7/pages/page_1.png"); err != nil {
		t.Fatalf("удаление через обёртку достало боевой: %v", err)
	}
	if _, _, err := prod.Head(ctx, "works/8/pages/page_1.png"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("запись через обёртку легла в боевой: %v", err)
	}
}

// Ответ «префикс пуст» помнится минуту: второе чтение того же тома в
// местный не ходит, после минуты — спрашивает заново.
func TestFallbackCachesPrefixForAMinute(t *testing.T) {
	local, prod, f := fbPair()
	clock := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return clock }
	fbPut(t, prod, "works/7/pages/page_1.png", "prod")
	fbPut(t, prod, "works/7/pages/page_2.png", "prod")

	fbRead(t, f, "works/7/pages/page_1.png")
	fbRead(t, f, "works/7/pages/page_2.png")
	if local.asked != 1 {
		t.Fatalf("за минуту спросили о префиксе %d раз, ждали 1", local.asked)
	}
	clock = clock.Add(61 * time.Second)
	fbRead(t, f, "works/7/pages/page_1.png")
	if local.asked != 2 {
		t.Fatalf("после минуты спросили %d раз, ждали 2", local.asked)
	}
}

// Новый том заведён локально через обёртку сразу после чтения «пусто»:
// запись забывает закэшированный ответ, и полоса видна сразу.
func TestFallbackPutForgetsCachedEmptyPrefix(t *testing.T) {
	_, prod, f := fbPair()
	clock := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return clock }
	fbPut(t, prod, "works/9/pages/page_1.png", "prod")

	fbRead(t, f, "works/9/pages/page_1.png")
	fbPut(t, f, "works/9/pages/page_1.png", "local")
	if got := fbRead(t, f, "works/9/pages/page_1.png"); got != "local" {
		t.Fatalf("после записи читается %q, ждали местную полосу без ожидания минуты", got)
	}
}

// Местный SeaweedFS лежит — ошибка наружу, а не тихий уход в боевой.
func TestFallbackPrimaryListingErrorIsNotHiddenBehindSecondary(t *testing.T) {
	local, prod, f := fbPair()
	local.err = errors.New("seaweedfs недоступен")
	fbPut(t, prod, "works/7/pages/page_1.png", "prod")

	if _, err := f.Get(context.Background(), "works/7/pages/page_1.png"); !errors.Is(err, local.err) {
		t.Fatalf("Get: %v; ждали ошибку местного хранилища", err)
	}
	if _, err := f.PresignGet(context.Background(), "works/7/pages/page_1.png", time.Minute); !errors.Is(err, local.err) {
		t.Fatalf("PresignGet: %v; ждали ошибку местного хранилища", err)
	}
}

// Объекта нет нигде — ErrObjectNotFound, как у любого хранилища.
func TestFallbackMissingEverywhereIsNotFound(t *testing.T) {
	local, _, f := fbPair()
	fbPut(t, local, "works/7/pages/page_1.png", "local")

	if _, _, err := f.Head(context.Background(), "works/7/pages/page_9.png"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("Head: %v; ждали ErrObjectNotFound", err)
	}
}
