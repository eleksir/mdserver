package main

import (
	"fmt"
	"github.com/gosidekick/goconfig"
	_ "github.com/gosidekick/goconfig/ini"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/bmizerany/pat"
	"github.com/russross/blackfriday"
)

const (
	configFileName = "mdserver.ini"
)

// Config - структура для считывания конфигурационного файла
type Config struct {
	Listen  string `ini:"listen" cfgDefault:"127.0.0.1:3000"`
	Static  string `ini:"static_dir" cfgDefault:"./public/static"`
	Uploads string `ini:"upload_dir" cfgDefault:"./public/uploads"`
}

type post struct {
	Title   string
	Body    template.HTML
	ModTime int64
}

type postArray struct {
	Items map[string]post
	sync.RWMutex
}

var (
	// Компилируем шаблоны, если не удалось, то выходим
	postTemplate  = template.Must(template.ParseFiles(path.Join("templates", "layout.html"), path.Join("templates", "post.html")))
	errorTemplate = template.Must(template.ParseFiles(path.Join("templates", "layout.html"), path.Join("templates", "error.html")))
	posts         = newPostArray()
)

func main() {
	cfg, err := readConfig(configFileName)

	if err != nil {
		log.Fatalln(err)
	}

	// Для отдачи сервером статичных файлов из папки public/static
	fs := noDirListing(http.FileServer(http.Dir("./public/static")))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	uploads := noDirListing(http.FileServer(http.Dir("./public/uploads")))
	http.Handle("/uploads/", http.StripPrefix("/uploads/", uploads))

	mux := pat.New()
	mux.Get("/:page", http.HandlerFunc(postHandler))
	mux.Get("/:page/", http.HandlerFunc(postHandler))
	mux.Get("/", http.HandlerFunc(postHandler))

	http.Handle("/", mux)
	log.Printf("Listening %s...", cfg.Listen)
	log.Fatalln(http.ListenAndServe(cfg.Listen, nil))
}

func postHandler(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	// Извлекаем параметр
	// Например, в http://127.0.0.1:3000/p1 page = "p1"
	// в http://127.0.0.1:3000/ page = ""
	page := params.Get(":page")
	// Путь к файлу (без расширения)
	// Например, posts/p1
	p := path.Join("posts", page)

	var postMD string

	if page != "" {
		// если page не пусто, то считаем, что запрашивается файл
		// получим posts/p1.md
		postMD = p + ".md"
	} else {
		// если page пусто, то выдаем главную
		postMD = p + "/index.md"
	}

	post, status, err := posts.Get(postMD)

	if err != nil {
		errorHandler(w, r, status)

		return
	}

	err = postTemplate.ExecuteTemplate(w, "layout", post)

	if err != nil {
		log.Println(err.Error())
		errorHandler(w, r, 500)
	}
}

func errorHandler(w http.ResponseWriter, r *http.Request, status int) {
	log.Printf("error %d %s %s\n", status, r.RemoteAddr, r.URL.Path)
	w.WriteHeader(status)

	err := errorTemplate.ExecuteTemplate(w, "layout", map[string]interface{}{"Error": http.StatusText(status), "Status": status})

	if err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), 500)

		return
	}
}

// Обёртка для http.FileServer, чтобы она не выдавала список файлов
// например, если открыть http://127.0.0.1:3000/static/,
// то будет видно список файлов внутри каталога.
// noDirListing - вернет 404 ошибку в этом случае.
func noDirListing(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") || r.URL.Path == "" {
			http.NotFound(w, r)

			return
		}

		h.ServeHTTP(w, r)
	}
}

func readConfig(configName string) (conf *Config, err error) {
	var cfg Config
	goconfig.File = configName
	err = goconfig.Parse(&cfg)

	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func newPostArray() *postArray {
	p := postArray{}
	p.Items = make(map[string]post)

	return &p
}

// Get Загружает markdown-файл и конвертирует его в HTML
// Возвращает объект типа Post
// Если путь не существует или является каталогом, то возвращаем ошибку
func (p *postArray) Get(md string) (post, int, error) {
	info, err := os.Stat(md)

	if err != nil {
		if os.IsNotExist(err) {
			// файл не существует
			return post{}, 404, err
		}

		return post{}, 500, err
	}

	if info.IsDir() {
		// не файл, а папка
		return post{}, 404, fmt.Errorf("dir")
	}

	val, ok := p.Items[md]

	if !ok || (ok && val.ModTime != info.ModTime().UnixNano()) {
		p.RLock()
		defer p.RUnlock()
		data, _ := ioutil.ReadFile(md)
		lines := strings.Split(string(data), "\n")
		title := lines[0]
		body := strings.Join(lines[1:], "\n")
		body = string(blackfriday.MarkdownCommon([]byte(body)))
		var tmpl = template.HTML(body)
		p.Items[md] = post{title, tmpl, info.ModTime().UnixNano()}
	}

	post := p.Items[md]

	return post, 200, nil
}
