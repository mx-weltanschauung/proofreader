package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMemoryStoragePresignGetAttachmentCarriesDisposition(t *testing.T) {
	disp := `attachment; filename="a.opus"`
	got, err := NewMemoryStorage().PresignGetAttachment(context.Background(), "works/1/a.opus", time.Hour, disp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "works/1/a.opus") || !strings.Contains(got, "disposition="+url.QueryEscape(disp)) {
		t.Errorf("ссылка = %q", got)
	}
}

func TestOriginalKey(t *testing.T) {
	got := OriginalKey(7, "Book.pdf", 1700000000)
	want := "works/7/original/1700000000_Book.pdf"
	if got != want {
		t.Errorf("OriginalKey = %q, want %q", got, want)
	}
}

func TestPagePreviewKey(t *testing.T) {
	got := PagePreviewKey(7, 12)
	want := "works/7/pages/page_12.png"
	if got != want {
		t.Errorf("PagePreviewKey = %q, want %q", got, want)
	}
}

func TestMemoryStoragePutGet(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStorage()
	if err := s.Put(ctx, "k1", bytes.NewBufferString("hello"), 5, "text/plain"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, err := s.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "hello" {
		t.Errorf("Get = %q, want %q", data, "hello")
	}
}

func TestMemoryStorageGetMissing(t *testing.T) {
	if _, err := NewMemoryStorage().Get(context.Background(), "nope"); err == nil {
		t.Error("expected error getting missing key")
	}
}

func TestMemoryStoragePresignGet(t *testing.T) {
	url, err := NewMemoryStorage().PresignGet(context.Background(), "works/1/pages/page_2.png", 30*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty presigned URL")
	}
}

func TestMemoryStorageDeletePrefix(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStorage()
	_ = s.Put(ctx, "works/1/a", bytes.NewBufferString("x"), 1, "")
	_ = s.Put(ctx, "works/1/b", bytes.NewBufferString("y"), 1, "")
	_ = s.Put(ctx, "works/2/c", bytes.NewBufferString("z"), 1, "")
	if err := s.DeletePrefix(ctx, "works/1/"); err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}
	if _, err := s.Get(ctx, "works/1/a"); err == nil {
		t.Error("works/1/a should be deleted")
	}
	if _, err := s.Get(ctx, "works/2/c"); err != nil {
		t.Error("works/2/c should survive")
	}
}

func TestMemoryStorageHeadReportsSizeAndMD5(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStorage()
	if err := m.Put(ctx, "a/b.opus", strings.NewReader("звук"), -1, "audio/ogg"); err != nil {
		t.Fatal(err)
	}
	size, etag, err := m.Head(ctx, "a/b.opus")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	sum := md5.Sum([]byte("звук"))
	if size != int64(len("звук")) || etag != hex.EncodeToString(sum[:]) {
		t.Errorf("Head = (%d, %q), ждали (%d, %q)", size, etag, len("звук"), hex.EncodeToString(sum[:]))
	}
}

func TestMemoryStorageHeadMissingIsErrObjectNotFound(t *testing.T) {
	_, _, err := NewMemoryStorage().Head(context.Background(), "нет")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("Head на пустом = %v, ждали ErrObjectNotFound", err)
	}
}

func TestMemoryStoragePresignPutCarriesKeyAndType(t *testing.T) {
	url, err := NewMemoryStorage().PresignPut(context.Background(), "k.opus", "audio/ogg", time.Minute)
	if err != nil || !strings.Contains(url, "k.opus") || !strings.Contains(url, "audio/ogg") {
		t.Errorf("PresignPut = %q, %v", url, err)
	}
}
