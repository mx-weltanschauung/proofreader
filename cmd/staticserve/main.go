// staticserve — сервер статической читальни, который едет в архиве рядом с
// index.html: двойной щелчок поднимает читальню на 127.0.0.1 и открывает
// браузер. Нужен ради поиска по тексту — pagefind грузит индекс fetch'ем, а
// через file:// браузер этого не даёт. Наружу не слушает.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"proofreader/internal/site"
	"runtime"
)

func main() {
	dir := flag.String("dir", "", "папка читальни; пусто — папка, где лежит программа")
	noBrowser := flag.Bool("no-browser", false, "не открывать браузер")
	flag.Parse()

	root := *dir
	if root == "" {
		exe, err := os.Executable()
		if err != nil {
			log.Fatal(err)
		}
		root = filepath.Dir(exe)
	}
	if err := checkRoot(root); err != nil {
		log.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	addr := "http://" + ln.Addr().String() + "/"
	fmt.Println(site.Name()+" открыта по адресу", addr)
	fmt.Println("Закройте это окно, чтобы остановить.")
	if !*noBrowser {
		openBrowser(addr)
	}
	log.Fatal(http.Serve(ln, handler(root)))
}

func checkRoot(root string) error {
	if _, err := os.Stat(filepath.Join(root, "index.html")); err != nil {
		return errors.New("в " + root + " нет index.html — положите программу в папку читальни")
	}
	return nil
}

func handler(root string) http.Handler {
	// Индекс pagefind — WebAssembly; без этого типа браузер его не компилирует.
	_ = mime.AddExtensionType(".wasm", "application/wasm")
	return http.FileServer(http.Dir(root))
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
