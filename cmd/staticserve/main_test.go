package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandlerServesFilesWithWasmType(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>Читальня</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pagefind"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pagefind", "x.wasm"), []byte{0, 'a', 's', 'm'}, 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler(dir))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "<p>Читальня</p>" {
		t.Errorf("корень отдал %q", body)
	}

	resp, err = http.Get(srv.URL + "/pagefind/x.wasm")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/wasm" {
		t.Errorf("Content-Type wasm = %q — браузер откажется компилировать индекс pagefind", ct)
	}
}

func TestRootRequiresIndex(t *testing.T) {
	if err := checkRoot(t.TempDir()); err == nil {
		t.Fatal("каталог без index.html принят за читальню")
	}
}
