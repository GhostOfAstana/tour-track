// Package web показывает каталог, объясняет оценки и обрабатывает действия посетителя.
package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"tour-track/internal/activity"
	"tour-track/internal/cache"
	"tour-track/internal/catalog"
	"tour-track/internal/seed"
)

//go:embed templates/*.html static/*
var assets embed.FS

type artistView struct {
	catalog.Artist
	Activity    activity.Result `json:"activity"`
	ImageCredit *seed.Credit    `json:"image_credit,omitempty"`
}

type pageData struct {
	Title, Query, Label, Sort, Error string
	Artists                          []artistView
	Artist                           *artistView
	Total                            int
	Updated                          string
	Stale                            bool
}

type App struct {
	store     *cache.Store
	templates *template.Template
	static    http.Handler
	images    http.Handler
	logger    *slog.Logger
	now       func() time.Time
}

func New(store *cache.Store, logger *slog.Logger) (*App, error) {
	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("прочитать шаблоны: %w", err)
	}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &App{store: store, templates: templates, static: http.FileServer(http.FS(static)), images: http.FileServer(http.FS(seed.Images())), logger: logger, now: time.Now}, nil
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https:; style-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("Cache-Control", "no-store")
	defer func() {
		if recovered := recover(); recovered != nil {
			a.logger.Error("сбой HTTP-обработчика", "path", r.URL.Path)
			a.fail(w, r, http.StatusInternalServerError, "Произошла внутренняя ошибка. Попробуйте ещё раз.")
		}
	}()
	if len(r.URL.RequestURI()) > 4096 {
		a.fail(w, r, http.StatusRequestURITooLong, "Адрес запроса слишком длинный.")
		return
	}
	if !utf8.ValidString(r.URL.RawQuery) {
		a.fail(w, r, http.StatusBadRequest, "Некорректная строка поиска.")
		return
	}
	if r.URL.Path == "/api/refresh" {
		if !a.method(w, r, http.MethodPost) {
			return
		}
		a.refresh(w, r)
		return
	}
	if !a.method(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	switch {
	case r.URL.Path == "/":
		a.index(w, r)
	case r.URL.Path == "/healthz":
		a.json(w, r, 200, map[string]string{"status": "ok"})
	case r.URL.Path == "/readyz":
		status := a.store.Status()
		code := 200
		if status.Count == 0 {
			code = 503
		}
		a.json(w, r, code, status)
	case r.URL.Path == "/api/status":
		a.json(w, r, 200, a.store.Status())
	case r.URL.Path == "/api/artists":
		a.index(w, r)
	case strings.HasPrefix(r.URL.Path, "/artists/"):
		a.detail(w, r, strings.TrimPrefix(r.URL.Path, "/artists/"))
	case strings.HasPrefix(r.URL.Path, "/api/artists/"):
		a.detail(w, r, strings.TrimPrefix(r.URL.Path, "/api/artists/"))
	case strings.HasPrefix(r.URL.Path, "/images/"):
		name := strings.TrimPrefix(r.URL.Path, "/images/")
		if strings.ContainsAny(name, "/\\") || (!strings.HasSuffix(name, ".jpg") && !strings.HasSuffix(name, ".png") && !strings.HasSuffix(name, ".webp")) {
			a.fail(w, r, 404, "Изображение не найдено.")
			return
		}
		if info, err := fs.Stat(seed.Images(), name); err != nil || info.IsDir() {
			a.fail(w, r, 404, "Изображение не найдено.")
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.StripPrefix("/images/", a.images).ServeHTTP(w, r)
	case strings.HasPrefix(r.URL.Path, "/static/"):
		name := strings.TrimPrefix(r.URL.Path, "/static/")
		if name != "style.css" && name != "app.js" {
			a.fail(w, r, 404, "Такого файла нет.")
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.StripPrefix("/static/", a.static).ServeHTTP(w, r)
	default:
		a.fail(w, r, 404, "Такой страницы нет. Выберите исполнителя в каталоге.")
	}
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	params, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		a.fail(w, r, 400, "Некорректные параметры запроса.")
		return
	}
	query := strings.TrimSpace(params.Get("q"))
	label := params.Get("label")
	order := params.Get("sort")
	if order == "" {
		order = "name"
	}
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 200 {
		a.fail(w, r, 400, "Поиск должен содержать не больше 200 символов.")
		return
	}
	if label != "" && label != "Emerging" && label != "Active" && label != "Established" && label != "Legendary" && label != "Rising Star" {
		a.fail(w, r, 400, "Неизвестная метка активности.")
		return
	}
	if order != "name" && order != "score" && order != "year" {
		a.fail(w, r, 400, "Неизвестный порядок сортировки.")
		return
	}
	snapshot := a.store.Snapshot()
	if len(snapshot.Artists) == 0 {
		a.fail(w, r, 503, "API временно недоступен, а сохранённых данных пока нет. Попробуйте позже.")
		return
	}
	now := a.now() // Один момент времени для всех оценок на странице, заново при каждом запросе.
	views := make([]artistView, 0, len(snapshot.Artists))
	for _, artist := range snapshot.Artists {
		view := makeView(artist, now)
		if !matches(artist, query) {
			continue
		}
		if label == "Rising Star" {
			if !view.Activity.RisingStar {
				continue
			}
		} else if label != "" && label != view.Activity.Label {
			continue
		}
		views = append(views, view)
	}
	sort.SliceStable(views, func(i, j int) bool {
		if order == "score" && views[i].Activity.Score != views[j].Activity.Score {
			return views[i].Activity.Score > views[j].Activity.Score
		}
		if order == "year" && views[i].CreationDate != views[j].CreationDate {
			return views[i].CreationDate > views[j].CreationDate
		}
		return strings.ToLower(views[i].Name) < strings.ToLower(views[j].Name)
	})
	data := pageData{Title: "Музыка в движении", Artists: views, Total: len(snapshot.Artists), Query: query, Label: label, Sort: order, Updated: formatTime(snapshot.UpdatedAt), Stale: snapshot.Stale}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		a.json(w, r, 200, map[string]any{"artists": views, "total": data.Total, "stale": data.Stale, "updated_at": snapshot.UpdatedAt})
		return
	}
	a.render(w, r, 200, "index", data)
}

func matches(artist catalog.Artist, query string) bool {
	if query == "" {
		return true
	}
	fields := append([]string{artist.Name, strconv.Itoa(artist.CreationDate), artist.FirstAlbum}, artist.Members...)
	fields = append(fields, artist.Locations...)
	query = strings.ToLower(query)
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func (a *App) detail(w http.ResponseWriter, r *http.Request, rawID string) {
	id, err := strconv.Atoi(rawID)
	if err != nil || id < 1 || strconv.Itoa(id) != rawID {
		a.fail(w, r, 404, "Исполнитель с таким номером не найден.")
		return
	}
	snapshot := a.store.Snapshot()
	if len(snapshot.Artists) == 0 {
		a.fail(w, r, 503, "Каталог временно недоступен. Попробуйте позже.")
		return
	}
	for _, artist := range snapshot.Artists {
		if artist.ID != id {
			continue
		}
		view := makeView(artist, a.now())
		if strings.HasPrefix(r.URL.Path, "/api/") {
			a.json(w, r, 200, view)
			return
		}
		a.render(w, r, 200, "detail", pageData{Title: artist.Name, Artist: &view, Updated: formatTime(snapshot.UpdatedAt), Stale: snapshot.Stale})
		return
	}
	a.fail(w, r, 404, "Исполнитель с таким номером не найден.")
}

func (a *App) refresh(w http.ResponseWriter, r *http.Request) {
	// Браузер с другого сайта не должен запускать обновления от имени посетителя.
	origin := r.Header.Get("Origin")
	if origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != r.Host || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			a.fail(w, r, 403, "Обновление доступно только с этого сайта.")
			return
		}
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		a.fail(w, r, 403, "Обновление доступно только с этого сайта.")
		return
	}
	if r.ContentLength > 1024 {
		a.fail(w, r, 413, "Тело запроса слишком большое.")
		return
	}
	// Ограничение действует и для chunked-запроса без известной длины.
	if r.Body != nil {
		defer r.Body.Close()
		if _, err := io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 1024)); err != nil {
			var limitError *http.MaxBytesError
			if errors.As(err, &limitError) {
				a.fail(w, r, 413, "Тело запроса слишком большое.")
			} else {
				a.fail(w, r, 400, "Не удалось прочитать запрос.")
			}
			return
		}
	}
	if !a.store.RequestRefresh() {
		w.Header().Set("Retry-After", "10")
		a.fail(w, r, 429, "Подождите несколько секунд перед следующим обновлением.")
		return
	}
	a.json(w, r, 202, map[string]bool{"refreshing": true})
}

func (a *App) method(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, method := range allowed {
		if r.Method == method {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	a.fail(w, r, 405, "Этот HTTP-метод здесь не поддерживается.")
	return false
}

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name string, data pageData) {
	// Сначала шаблон полностью собирается в памяти: ошибка не оставит половину страницы с кодом 200.
	var buffer bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buffer, name, data); err != nil {
		a.logger.Error("ошибка шаблона", "template", name)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(500)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte("Внутренняя ошибка отображения\n"))
		}
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(buffer.Bytes())
	}
}

func (a *App) json(w http.ResponseWriter, r *http.Request, status int, data any) {
	body, err := json.Marshal(data)
	if err != nil {
		http.Error(w, "Внутренняя ошибка JSON", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func (a *App) fail(w http.ResponseWriter, r *http.Request, status int, message string) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		a.json(w, r, status, map[string]any{"error": message, "status": status})
		return
	}
	a.render(w, r, status, "error", pageData{Title: strconv.Itoa(status), Error: message})
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "резервный снимок"
	}
	return value.UTC().Format("02.01.2006 15:04 UTC")
}

// Атрибуция относится к локальной иллюстрации Commons, а не к внешней фотографии API.
func makeView(artist catalog.Artist, now time.Time) artistView {
	result := artistView{Artist: artist, Activity: activity.Analyze(artist.Locations, artist.Dates, artist.CreationDate, now)}
	if strings.HasPrefix(artist.Image, "/images/") {
		if credit, ok := seed.ImageCredit(artist.ID); ok {
			result.ImageCredit = &credit
		}
	}
	return result
}
