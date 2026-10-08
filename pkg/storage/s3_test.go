package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Ссылка «скачать» несёт response-content-disposition в подписи: без него
// presigned GET с чужого домена браузер открывает и играет во вкладке, а
// атрибут download на кросс-доменной ссылке игнорируется.
func TestS3PresignGetAttachmentSignsDisposition(t *testing.T) {
	s, err := NewS3Storage(S3Config{
		Endpoint: "http://localhost:8333", Region: "us-east-1", Bucket: "proofreader-audio",
		AccessKey: "k", SecretKey: "sec", UsePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	disp := `attachment; filename="track-3.opus"; filename*=UTF-8''%D0%A2.opus`
	got, err := s.PresignGetAttachment(context.Background(), "works/1/a.opus", time.Hour, disp)
	if err != nil {
		t.Fatalf("PresignGetAttachment: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("response-content-disposition") != disp {
		t.Errorf("response-content-disposition = %q, ждали %q", u.Query().Get("response-content-disposition"), disp)
	}
}

// Проба на живом SeaweedFS: подписанный заголовок доезжает до ответа GET.
func TestS3PresignGetAttachmentServesDisposition(t *testing.T) {
	endpoint := os.Getenv("PROOFREADER_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set PROOFREADER_S3_TEST_ENDPOINT to run S3 integration test")
	}
	ctx := context.Background()
	s, err := NewS3Storage(S3Config{
		Endpoint: endpoint, Region: "us-east-1", Bucket: "proofreader-audio-test",
		AccessKey:    os.Getenv("PROOFREADER_S3_TEST_ACCESS_KEY"),
		SecretKey:    os.Getenv("PROOFREADER_S3_TEST_SECRET_KEY"),
		UsePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(ctx); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	key := "works/999/disposition-probe.opus"
	t.Cleanup(func() { _ = s.Delete(context.Background(), key) })
	if err := s.Put(ctx, key, bytes.NewBufferString("opus"), 4, "audio/ogg"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	disp := `attachment; filename="track-1.opus"; filename*=UTF-8''%D0%A2%D0%BE%D0%B2%D0%B0%D1%80.opus`
	link, err := s.PresignGetAttachment(ctx, key, time.Minute, disp)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Disposition") != disp {
		t.Errorf("GET = %d, Content-Disposition %q", resp.StatusCode, resp.Header.Get("Content-Disposition"))
	}
}

// TestS3StorageRoundTrip runs only when PROOFREADER_S3_TEST_ENDPOINT is set,
// e.g. against a local SeaweedFS/MinIO. It is skipped in normal CI.
func TestS3StorageRoundTrip(t *testing.T) {
	endpoint := os.Getenv("PROOFREADER_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set PROOFREADER_S3_TEST_ENDPOINT to run S3 integration test")
	}
	ctx := context.Background()
	s, err := NewS3Storage(S3Config{
		Endpoint:     endpoint,
		Region:       "us-east-1",
		Bucket:       "proofreader-test",
		AccessKey:    os.Getenv("PROOFREADER_S3_TEST_ACCESS_KEY"),
		SecretKey:    os.Getenv("PROOFREADER_S3_TEST_SECRET_KEY"),
		UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewS3Storage: %v", err)
	}
	if err := s.EnsureBucket(ctx); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	key := "works/999/pages/page_1.png"
	if err := s.Put(ctx, key, bytes.NewBufferString("data"), 4, "image/png"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "data" {
		t.Errorf("Get = %q, want %q", got, "data")
	}
	url, err := s.PresignGet(ctx, key, 15*time.Minute)
	if err != nil || url == "" {
		t.Fatalf("PresignGet: url=%q err=%v", url, err)
	}
	if err := s.DeletePrefix(ctx, "works/999/"); err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}
}

func TestS3StoragePresignUsesPublicEndpoint(t *testing.T) {
	s, err := NewS3Storage(S3Config{
		Endpoint:       "http://seaweedfs:8333",
		PublicEndpoint: "http://localhost:8333",
		Region:         "us-east-1",
		Bucket:         "proofreader",
		AccessKey:      "k",
		SecretKey:      "sec",
		UsePathStyle:   true,
	})
	if err != nil {
		t.Fatalf("NewS3Storage: %v", err)
	}
	url, err := s.PresignGet(context.Background(), "works/1/pages/page_1.png", time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	if !strings.Contains(url, "localhost:8333") {
		t.Errorf("presigned URL should target public endpoint, got %q", url)
	}
	if strings.Contains(url, "seaweedfs:8333") {
		t.Errorf("presigned URL must not leak internal endpoint, got %q", url)
	}
}

func TestS3StoragePresignDefaultsToEndpoint(t *testing.T) {
	s, err := NewS3Storage(S3Config{
		Endpoint:     "http://localhost:8333",
		Region:       "us-east-1",
		Bucket:       "proofreader",
		AccessKey:    "k",
		SecretKey:    "sec",
		UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewS3Storage: %v", err)
	}
	url, err := s.PresignGet(context.Background(), "works/1/pages/page_1.png", time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	if !strings.Contains(url, "localhost:8333") {
		t.Errorf("empty PublicEndpoint should fall back to Endpoint, got %q", url)
	}
}

// TestS3PresignPutThenHead — проба риска спеки аудиокниг: ETag однократного
// PUT у SeaweedFS обязан быть md5 тела, иначе регистрация дорожек сверяет
// не то. Presigned PUT идёт голым http.Client: заголовок Authorization в
// запросе по подписанной ссылке S3 отвергает.
func TestS3PresignPutThenHead(t *testing.T) {
	endpoint := os.Getenv("PROOFREADER_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set PROOFREADER_S3_TEST_ENDPOINT to run S3 integration test")
	}
	ctx := context.Background()
	s, err := NewS3Storage(S3Config{
		Endpoint: endpoint, Region: "us-east-1", Bucket: "proofreader-audio-test",
		AccessKey:    os.Getenv("PROOFREADER_S3_TEST_ACCESS_KEY"),
		SecretKey:    os.Getenv("PROOFREADER_S3_TEST_SECRET_KEY"),
		UsePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(ctx); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	key := "works/999/abcdef01/1-2-12345678.opus"
	t.Cleanup(func() { _ = s.Delete(context.Background(), key) })
	body := bytes.Repeat([]byte("opus"), 300_000) // 1,2 МБ — больше одного куска
	url, err := s.PresignPut(ctx, key, "audio/ogg", 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "audio/ogg")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %d", resp.StatusCode)
	}
	size, etag, err := s.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	sum := md5.Sum(body)
	if size != int64(len(body)) || etag != hex.EncodeToString(sum[:]) {
		t.Errorf("Head = (%d, %q), ждали (%d, %q)", size, etag, len(body), hex.EncodeToString(sum[:]))
	}
	if _, _, err := s.Head(ctx, key+".нет"); !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("Head на отсутствующем = %v, ждали ErrObjectNotFound", err)
	}
}

// corsServer — подставной S3: запоминает каждый запрос и отвечает status.
func corsServer(t *testing.T, status int) (*httptest.Server, *[]*http.Request, *[]string) {
	t.Helper()
	var reqs []*http.Request
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		reqs = append(reqs, r)
		bodies = append(bodies, string(b))
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs, &bodies
}

func corsStore(t *testing.T, endpoint string) *S3Storage {
	t.Helper()
	s, err := NewS3Storage(S3Config{
		Endpoint: endpoint, Region: "default", Bucket: "proofreader-audio",
		AccessKey: "k", SecretKey: "sec", UsePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Запись человека заливается presigned PUT прямо из браузера; внешний S3 без
// правил CORS отвечает на preflight 403 (проба s3.example.org 02.10.2026).
func TestS3SetCORSSendsRules(t *testing.T) {
	srv, reqs, bodies := corsServer(t, http.StatusOK)
	s := corsStore(t, srv.URL)
	if err := s.SetCORS(context.Background(), []string{"https://lib.example.org"}); err != nil {
		t.Fatalf("SetCORS: %v", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("запросов %d, ждали 1", len(*reqs))
	}
	r, body := (*reqs)[0], (*bodies)[0]
	if r.Method != http.MethodPut || r.URL.Path != "/proofreader-audio" || !r.URL.Query().Has("cors") {
		t.Errorf("запрос %s %s, ждали PUT /proofreader-audio?cors", r.Method, r.URL.String())
	}
	for _, want := range []string{
		"<AllowedOrigin>https://lib.example.org</AllowedOrigin>",
		"<AllowedMethod>PUT</AllowedMethod>",
		"<AllowedMethod>GET</AllowedMethod>",
		"<AllowedMethod>HEAD</AllowedMethod>",
		"<AllowedHeader>*</AllowedHeader>",
		"<ExposeHeader>ETag</ExposeHeader>",
		"<MaxAgeSeconds>3600</MaxAgeSeconds>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в теле нет %s:\n%s", want, body)
		}
	}
}

// Пустой список — местный SeaweedFS: он отражает любой origin сам, и трогать
// его настройки незачем.
func TestS3SetCORSEmptyDoesNothing(t *testing.T) {
	srv, reqs, _ := corsServer(t, http.StatusOK)
	s := corsStore(t, srv.URL)
	if err := s.SetCORS(context.Background(), nil); err != nil {
		t.Fatalf("SetCORS(nil): %v", err)
	}
	if len(*reqs) != 0 {
		t.Errorf("запросов %d, ждали 0", len(*reqs))
	}
}

// Отказ провайдера доходит до вызывающего: main пишет его в журнал, а не
// глотает молча.
func TestS3SetCORSReturnsProviderError(t *testing.T) {
	srv, _, _ := corsServer(t, http.StatusForbidden)
	s := corsStore(t, srv.URL)
	if err := s.SetCORS(context.Background(), []string{"https://lib.example.org"}); err == nil {
		t.Error("SetCORS на 403 вернул nil")
	}
}

// HasPrefix спрашивает ровно один ключ: обёртке Fallback нужно «есть ли под
// works/N/ хоть что-то», а не листинг тома в 800 полос.
func TestS3HasPrefixAsksForOneKey(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>proofreader-audio</Name><Prefix>works/7/</Prefix><KeyCount>1</KeyCount><MaxKeys>1</MaxKeys><IsTruncated>true</IsTruncated><Contents><Key>works/7/pages/page_1.png</Key><Size>3</Size></Contents></ListBucketResult>`)
	}))
	defer srv.Close()

	ok, err := corsStore(t, srv.URL).HasPrefix(context.Background(), "works/7/")
	if err != nil || !ok {
		t.Fatalf("HasPrefix = %v, %v; ждали true", ok, err)
	}
	q := got.URL.Query()
	if q.Get("max-keys") != "1" || q.Get("prefix") != "works/7/" || q.Get("list-type") != "2" {
		t.Fatalf("запрос %s: ждали list-type=2, prefix=works/7/, max-keys=1", got.URL.RawQuery)
	}
}

func TestS3HasPrefixEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>proofreader-audio</Name><Prefix>works/7/</Prefix><KeyCount>0</KeyCount><MaxKeys>1</MaxKeys><IsTruncated>false</IsTruncated></ListBucketResult>`)
	}))
	defer srv.Close()

	ok, err := corsStore(t, srv.URL).HasPrefix(context.Background(), "works/7/")
	if err != nil || ok {
		t.Fatalf("HasPrefix = %v, %v; ждали false", ok, err)
	}
}
