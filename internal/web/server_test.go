package web

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tour-track/internal/activity"
	"tour-track/internal/cache"
	"tour-track/internal/catalog"
	"tour-track/internal/seed"
)

type testFetchFunc func(context.Context) ([]catalog.Artist, error)

func (f testFetchFunc) Fetch(ctx context.Context) ([]catalog.Artist, error) { return f(ctx) }

func auditArtists() []catalog.Artist {
	return []catalog.Artist{
		{ID: 1, Name: "Queen", CreationDate: 1970, FirstAlbum: "13-07-1973", Image: "https://example.org/queen.jpg",
			Members:   []string{"Freddie Mercury", "Brian May", "John Daecon", "Roger Meddows-Taylor", "Mike Grose", "Barry Mitchell", "Doug Fogie"},
			Locations: []string{"london-uk", "boston-usa"}, Dates: []string{"01-01-2025", "02-01-2025", "03-01-2025", "04-01-2025", "05-01-2025", "06-01-2025", "07-01-2025", "08-01-2025"},
			Concerts: []catalog.Concert{{Location: "london-uk", Dates: []string{"01-01-2025"}}}},
		{ID: 2, Name: "Gorillaz", CreationDate: 1998, FirstAlbum: "26-03-2001", Members: []string{"Damon Albarn"}, Image: "https://example.org/gorillaz.jpg", Locations: []string{}, Dates: []string{}},
		{ID: 3, Name: "Travis Scott", CreationDate: 2008, FirstAlbum: "04-09-2015", Image: "https://example.org/travis.jpg", Members: []string{"Travis Scott"},
			Locations: []string{"santiago-chile", "sao_paulo-brasil", "los_angeles-usa", "houston-usa", "atlanta-usa", "new_orleans-usa", "philadelphia-usa", "london-uk", "frauenfeld-switzerland", "turku-finland"},
			Dates:     []string{"01-01-2025", "02-01-2025", "03-01-2025", "04-01-2025", "05-01-2025", "06-01-2025", "07-01-2025", "08-01-2025", "09-01-2025", "10-01-2025", "11-01-2025", "12-01-2025"}},
		{ID: 4, Name: "Foo Fighters", CreationDate: 1994, FirstAlbum: "04-07-1995", Image: "https://example.org/foo.jpg",
			Members:   []string{"Dave Grohl", "Nate Mendel", "Taylor Hawkins", "Chris Shiflett", "Pat Smear", "Rami Jaffee"},
			Locations: []string{"paris-france", "london-uk"}, Dates: []string{"*01-01-2025", "02-01-2025"}},
	}
}

func newTestApp(t *testing.T, artists []catalog.Artist) (*App, *cache.Store) {
	t.Helper()
	store := cache.New(testFetchFunc(func(context.Context) ([]catalog.Artist, error) { return artists, nil }), artists, time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC))
	app, err := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	app.now = func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }
	return app, store
}

func perform(app *App, method, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, httptest.NewRequest(method, target, nil))
	return recorder
}

func TestMainAndAuditDetailPages(t *testing.T) {
	artists := auditArtists()
	app, _ := newTestApp(t, artists)
	main := perform(app, http.MethodGet, "/")
	if main.Code != 200 || !strings.HasPrefix(main.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("главная страница: status=%d, headers=%v, body=%s", main.Code, main.Header(), main.Body.String())
	}
	for _, artist := range artists {
		if !strings.Contains(main.Body.String(), artist.Name) {
			t.Errorf("на главной нет %s", artist.Name)
		}
	}
	for _, label := range []string{"Emerging", "Active", "Established", "Legendary"} {
		if !strings.Contains(main.Body.String(), "badge-"+label) {
			t.Errorf("на карточках нет метки %s", label)
		}
	}
	for _, artist := range artists {
		t.Run(artist.Name, func(t *testing.T) {
			response := perform(app, http.MethodGet, "/artists/"+strconv.Itoa(artist.ID))
			if response.Code != 200 {
				t.Fatalf("карточка вернула %d: %s", response.Code, response.Body.String())
			}
			for _, expected := range append(append([]string{artist.Name, artist.FirstAlbum}, artist.Members...), artist.Locations...) {
				if !strings.Contains(response.Body.String(), expected) {
					t.Errorf("в карточке отсутствует исходное значение %q", expected)
				}
			}
			label := activity.Analyze(artist.Locations, artist.Dates, artist.CreationDate, app.now()).Label
			if !strings.Contains(response.Body.String(), "badge-"+label) {
				t.Errorf("в подробной карточке отсутствует метка %s", label)
			}
		})
	}
	queen := perform(app, http.MethodGet, "/artists/1").Body.String()
	if !strings.Contains(queen, "01-01-2025") || !strings.Contains(queen, "Rising Star") || !strings.Contains(queen, "Связи мест и дат из API") {
		t.Fatal("подробная карточка не показывает даты, relation или Rising Star")
	}
}

func TestAPIClassificationRecomputedAtRequestTime(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	var artist artistView
	response := perform(app, http.MethodGet, "/api/artists/2")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &artist) != nil {
		t.Fatalf("некорректный JSON карточки: %s", response.Body.String())
	}
	if artist.Activity.CareerYears != 28 || artist.Activity.Score != 5 || artist.Activity.Label != "Emerging" {
		t.Fatalf("2026: ожидались 28 лет, 5 баллов, Emerging, получено %+v", artist.Activity)
	}
	app.now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	response = perform(app, http.MethodGet, "/api/artists/2")
	if err := json.Unmarshal(response.Body.Bytes(), &artist); err != nil {
		t.Fatal(err)
	}
	if artist.Activity.CareerYears != 32 || artist.Activity.Score != 6 {
		t.Fatalf("оценка закеширована вместо пересчёта по году запроса: %+v", artist.Activity)
	}
	app.now = func() time.Time { return time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC) }
	response = perform(app, http.MethodGet, "/api/artists/3")
	if err := json.Unmarshal(response.Body.Bytes(), &artist); err != nil {
		t.Fatal(err)
	}
	if artist.Activity.TourSpread != 6 || artist.Activity.ConcertCount != 12 || artist.Activity.CareerYears != 18 || artist.Activity.Score != 51 || artist.Activity.Label != "Legendary" {
		t.Fatalf("Travis: ожидалась формула 6×2+12×3+3=51: %+v", artist.Activity)
	}
}

func TestSearchFiltersAndSort(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	tests := []struct {
		query string
		ids   []int
	}{
		{"q=FREDDIE", []int{1}}, {"q=" + url.QueryEscape("26-03-2001"), []int{2}},
		{"q=turku", []int{3}}, {"q=1994", []int{4}}, {"q=неизвестный", []int{}},
		{"label=Emerging", []int{2}}, {"label=Established", []int{1}},
		{"sort=score", []int{3, 1, 4, 2}}, {"sort=year", []int{3, 2, 4, 1}},
	}
	for _, test := range tests {
		t.Run(test.query, func(t *testing.T) {
			response := perform(app, http.MethodGet, "/api/artists?"+test.query)
			var body struct {
				Artists []artistView `json:"artists"`
				Total   int          `json:"total"`
			}
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil {
				t.Fatalf("поиск вернул %d: %s", response.Code, response.Body.String())
			}
			ids := make([]int, 0, len(body.Artists))
			for _, artist := range body.Artists {
				ids = append(ids, artist.ID)
			}
			if !reflect.DeepEqual(ids, test.ids) || body.Total != 4 {
				t.Fatalf("ids=%v, ожидались %v, total=%d", ids, test.ids, body.Total)
			}
		})
	}
}

func TestHTTPMethodsAndMissingRoutes(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	for _, test := range []struct{ method, target, allow string }{
		{http.MethodPost, "/", "GET, HEAD"}, {http.MethodPut, "/artists/1", "GET, HEAD"},
		{http.MethodDelete, "/api/artists", "GET, HEAD"}, {http.MethodGet, "/api/refresh", "POST"},
		{http.MethodHead, "/api/refresh", "POST"},
	} {
		response := perform(app, test.method, test.target)
		if response.Code != 405 || response.Header().Get("Allow") != test.allow {
			t.Errorf("%s %s: status=%d, Allow=%q", test.method, test.target, response.Code, response.Header().Get("Allow"))
		}
	}
	for _, target := range []string{"/missing", "/artists/999", "/artists/01", "/artists/-1", "/artists/no", "/artists/1/extra", "/api/artists/999", "/static/missing.js", "/static/", "/static/../server.go"} {
		if response := perform(app, http.MethodGet, target); response.Code != 404 {
			t.Errorf("несуществующий %s вернул %d", target, response.Code)
		}
	}
}

func TestRejectsMalformedAndOversizedQueries(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	for _, target := range []string{"/?q=%ZZ", "/?q=%FF", "/?q=a;b", "/?label=Unknown", "/?sort=Unknown", "/?q=" + url.QueryEscape(strings.Repeat("я", 201)), "/api/artists?q=%ZZ"} {
		if response := perform(app, http.MethodGet, target); response.Code != 400 {
			t.Errorf("некорректный %s вернул %d", target, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.URL.RawQuery = "q=" + string([]byte{0xff})
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != 400 {
		t.Errorf("сырой некорректный UTF-8 вернул %d", response.Code)
	}
	if response := perform(app, http.MethodGet, "/?q="+strings.Repeat("a", 5000)); response.Code != 414 {
		t.Errorf("слишком длинный адрес вернул %d", response.Code)
	}
	if response := perform(app, http.MethodGet, "/?q="+url.QueryEscape(strings.Repeat("я", 200))); response.Code != 200 {
		t.Errorf("допустимые 200 русских букв отклонены: %d", response.Code)
	}
}

func TestHTMLAutomaticallyEscapesUntrustedTextAndImageURL(t *testing.T) {
	artists := auditArtists()
	payload := `<script>alert("audit")</script>`
	artists[0].Name = payload
	artists[0].Members = []string{`"><img src=x onerror=alert(1)>`}
	artists[0].Image = "javascript:alert(1)"
	app, _ := newTestApp(t, artists)
	for _, target := range []string{"/", "/artists/1", "/?q=" + url.QueryEscape(payload)} {
		response := perform(app, http.MethodGet, target)
		body := response.Body.String()
		if response.Code != 200 || strings.Contains(body, payload) || strings.Contains(body, `<img src=x onerror=`) || strings.Contains(body, `src="javascript:`) {
			t.Fatalf("опасный текст попал в исполняемую HTML-разметку: %s", body)
		}
		if !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(response.Header().Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatal("отсутствует экранирование текста или ограничение скриптов")
		}
	}
}

func TestRefreshCSRFValidationAndAcceptedQueue(t *testing.T) {
	app, store := newTestApp(t, auditArtists())
	for _, test := range []struct{ origin, site string }{
		{"https://evil.example", ""}, {"null", ""}, {"http://example.com.evil", ""}, {"", "cross-site"},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
		request.Header.Set("Origin", test.origin)
		request.Header.Set("Sec-Fetch-Site", test.site)
		response := httptest.NewRecorder()
		app.ServeHTTP(response, request)
		if response.Code != 403 || store.Status().Refreshing {
			t.Fatalf("чужой сайт запустил обновление: origin=%q, site=%q, status=%d", test.origin, test.site, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
	request.Header.Set("Origin", "http://example.com")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != 202 || !store.Status().Refreshing || !strings.Contains(response.Body.String(), `"refreshing":true`) {
		t.Fatalf("задание не принято: status=%d body=%s state=%+v", response.Code, response.Body.String(), store.Status())
	}
	if repeated := perform(app, http.MethodPost, "/api/refresh"); repeated.Code != 202 {
		t.Fatalf("повторное нажатие должно присоединиться к существующему заданию: %d", repeated.Code)
	}
}

func TestRefreshRateLimitAfterCompletedJob(t *testing.T) {
	app, store := newTestApp(t, auditArtists())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	go func() { defer close(stopped); store.Run(ctx, time.Hour) }()
	defer func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
			t.Error("работник не остановился")
		}
	}()
	if response := perform(app, http.MethodPost, "/api/refresh"); response.Code != 202 {
		t.Fatal(response.Code)
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for store.Status().Refreshing {
		select {
		case <-deadline.C:
			t.Fatal("обновление зависло")
		case <-poll.C:
		}
	}
	response := perform(app, http.MethodPost, "/api/refresh")
	if response.Code != 429 || response.Header().Get("Retry-After") == "" {
		t.Fatalf("пауза между обновлениями не соблюдена: status=%d headers=%v", response.Code, response.Header())
	}
}

func TestRefreshRejectsOversizedBody(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	request := httptest.NewRequest(http.MethodPost, "/api/refresh", strings.NewReader(strings.Repeat("x", 1025)))
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != 413 {
		t.Fatalf("тело 1025 байт вернуло %d", response.Code)
	}
}

func TestHEADHasStatusAndHeadersWithoutBody(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	for _, target := range []string{"/", "/artists/1", "/api/artists", "/api/artists/1", "/api/status", "/healthz", "/readyz", "/static/style.css", "/static/app.js", "/missing", "/api/missing"} {
		get := perform(app, http.MethodGet, target)
		head := perform(app, http.MethodHead, target)
		if head.Code != get.Code || head.Body.Len() != 0 || head.Header().Get("Content-Type") != get.Header().Get("Content-Type") {
			t.Errorf("HEAD %s: status=%d/%d body=%d type=%q/%q", target, head.Code, get.Code, head.Body.Len(), head.Header().Get("Content-Type"), get.Header().Get("Content-Type"))
		}
	}
}

func TestStaticsAndHealthStatuses(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	for _, target := range []string{"/static/style.css", "/static/app.js"} {
		response := perform(app, http.MethodGet, target)
		if response.Code != 200 || response.Body.Len() == 0 || response.Header().Get("Cache-Control") != "public, max-age=3600" {
			t.Errorf("статический ресурс %s не обслуживается: %d", target, response.Code)
		}
	}
	for _, test := range []struct {
		target string
		want   int
	}{{"/api/status", 200}, {"/healthz", 200}, {"/readyz", 200}} {
		response := perform(app, http.MethodGet, test.target)
		if response.Code != test.want || !json.Valid(response.Body.Bytes()) {
			t.Errorf("%s: %d %s", test.target, response.Code, response.Body.String())
		}
	}
	empty, _ := newTestApp(t, nil)
	for _, test := range []struct {
		target string
		want   int
	}{{"/", 503}, {"/artists/1", 503}, {"/api/artists", 503}, {"/api/artists/1", 503}, {"/readyz", 503}, {"/healthz", 200}} {
		response := perform(empty, http.MethodGet, test.target)
		if response.Code != test.want {
			t.Errorf("без начальных данных %s: %d, ожидался %d", test.target, response.Code, test.want)
		}
	}
}

func TestTemplateFailureReturns500WithoutPartialHTML(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	app.templates = template.Must(template.New("broken").Parse(`{{define "index"}}PARTIAL-MARKER{{.FieldThatDoesNotExist}}{{end}}`))
	response := perform(app, http.MethodGet, "/")
	if response.Code != 500 || strings.Contains(response.Body.String(), "PARTIAL-MARKER") {
		t.Fatalf("ошибка шаблона опубликовала частичную страницу: %d %s", response.Code, response.Body.String())
	}
}

func TestConcurrentRequestsKeepCatalogIntact(t *testing.T) {
	app, store := newTestApp(t, auditArtists())
	before := store.Snapshot()
	var wg sync.WaitGroup
	targets := []string{"/", "/artists/1", "/api/artists?sort=score", "/api/artists/3", "/api/status", "/readyz", "/static/style.css"}
	for i := range 200 {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			response := perform(app, http.MethodGet, target)
			if response.Code != 200 {
				t.Errorf("параллельный %s вернул %d", target, response.Code)
			}
		}(targets[i%len(targets)])
	}
	wg.Wait()
	if after := store.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("чтение страниц изменило сохранённый каталог")
	}
}

func TestHandlerRecoversUnexpectedPanic(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	// Неожиданная внутренняя ошибка должна стать ответом 500, а не остановкой сервера.
	app.store = nil
	for _, target := range []string{"/", "/api/artists"} {
		response := perform(app, http.MethodGet, target)
		if response.Code != 500 {
			t.Fatalf("паника при %s не обработана: %d", target, response.Code)
		}
	}
}

func TestRefreshBodyLimitAppliesWithoutContentLength(t *testing.T) {
	for _, test := range []struct {
		name       string
		size, want int
	}{
		{"граница лимита", 1024, 202},
		{"превышение лимита", 1025, 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, store := newTestApp(t, auditArtists())
			request := httptest.NewRequest(http.MethodPost, "/api/refresh", io.NopCloser(strings.NewReader(strings.Repeat("x", test.size))))
			request.ContentLength = -1
			request.TransferEncoding = []string{"chunked"}
			response := httptest.NewRecorder()
			app.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("тело неизвестной длины %d байт вернуло %d, ожидалось %d", test.size, response.Code, test.want)
			}
			if test.want == 413 && store.Status().Refreshing {
				t.Fatal("отвергнутый запрос всё же поставил обновление в очередь")
			}
		})
	}
}

func TestHEADTemplateFailureHasNoBody(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	app.templates = template.Must(template.New("broken").Parse(`{{define "index"}}PARTIAL-MARKER{{.FieldThatDoesNotExist}}{{end}}`))
	response := perform(app, http.MethodHead, "/")
	if response.Code != 500 || response.Body.Len() != 0 {
		t.Fatalf("HEAD с ошибкой шаблона вернул %d и %d байт тела", response.Code, response.Body.Len())
	}
}

func TestRealHTTPMixedLoadDuringUpstreamOutage(t *testing.T) {
	started := make(chan struct{})
	releaseFailure := make(chan struct{})
	var startOnce, releaseOnce sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startOnce.Do(func() { close(started) })
		select {
		case <-releaseFailure:
			http.Error(w, "внешний источник недоступен", http.StatusServiceUnavailable)
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	defer releaseOnce.Do(func() { close(releaseFailure) })
	source, err := catalog.NewClient(upstream.URL, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	seed := auditArtists()
	capturedAt := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	store := cache.New(source, seed, capturedAt)
	initialSnapshot := store.Snapshot()
	app, err := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	app.now = func() time.Time { return capturedAt }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stopped := make(chan struct{})
	go func() { defer close(stopped); store.Run(ctx, time.Hour) }()
	defer func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
			t.Error("работник нагрузки не остановился")
		}
	}()
	store.RequestRefresh()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("внешний запрос не начался")
	}

	server := httptest.NewServer(app)
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	type operation struct {
		method, path string
		allowed      []int
	}
	operations := []operation{
		{http.MethodGet, "/", []int{200}}, {http.MethodGet, "/artists/1", []int{200}},
		{http.MethodGet, "/api/artists?sort=score", []int{200}}, {http.MethodGet, "/api/artists/3", []int{200}},
		{http.MethodGet, "/healthz", []int{200}}, {http.MethodGet, "/readyz", []int{200}},
		{http.MethodGet, "/api/status", []int{200}}, {http.MethodGet, "/static/style.css", []int{200}},
		{http.MethodGet, "/missing", []int{404}}, {http.MethodGet, "/api/artists?q=%FF", []int{400}},
		{http.MethodPost, "/api/refresh", []int{202, 429}}, {http.MethodGet, "/api/refresh", []int{405}},
		{http.MethodHead, "/artists/2", []int{200}}, {http.MethodHead, "/api/artists", []int{200}},
	}
	jobs := make(chan int, 1500)
	for i := range 1500 {
		jobs <- i
	}
	close(jobs)
	var completed atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				op := operations[i%len(operations)]
				request, err := http.NewRequestWithContext(ctx, op.method, server.URL+op.path, nil)
				if err != nil {
					t.Error(err)
					continue
				}
				response, err := client.Do(request)
				if err != nil {
					t.Errorf("%s %s: %v", op.method, op.path, err)
					continue
				}
				body, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
				response.Body.Close()
				acceptable := false
				for _, status := range op.allowed {
					if response.StatusCode == status {
						acceptable = true
					}
				}
				if !acceptable || readErr != nil || (op.method == http.MethodHead && len(body) != 0) {
					t.Errorf("%s %s: status=%d allowed=%v bytes=%d read=%v", op.method, op.path, response.StatusCode, op.allowed, len(body), readErr)
				}
				if completed.Add(1) == 400 {
					releaseOnce.Do(func() { close(releaseFailure) })
				}
			}
		}()
	}
	wg.Wait()
	releaseOnce.Do(func() { close(releaseFailure) })
	if completed.Load() != 1500 {
		t.Fatalf("выполнено %d запросов вместо 1500", completed.Load())
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for store.Status().Refreshing {
		select {
		case <-deadline.C:
			t.Fatal("отказ источника не завершил обновление")
		case <-poll.C:
		}
	}
	if status := store.Status(); status.LastError == "" || !status.Stale || status.Count != len(seed) {
		t.Fatalf("отказ источника не отражён в состоянии: %+v", status)
	}
	if snapshot := store.Snapshot(); !reflect.DeepEqual(snapshot.Artists, initialSnapshot.Artists) || !snapshot.UpdatedAt.Equal(capturedAt) {
		t.Fatal("под нагрузкой или при сетевом отказе был изменён последний полный каталог")
	}
	if response := perform(app, http.MethodGet, "/healthz"); response.Code != 200 {
		t.Fatal("после нагрузки сервер перестал отвечать")
	}
}

func TestLocalImagesServeExpectedBytesAndHEAD(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	for _, test := range []struct {
		file, mime string
		signature  []byte
	}{
		{"1.jpg", "image/jpeg", []byte{0xff, 0xd8, 0xff}},
		{"7.png", "image/png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}},
	} {
		t.Run(test.file, func(t *testing.T) {
			want, err := fs.ReadFile(seed.Images(), test.file)
			if err != nil {
				t.Fatal(err)
			}
			response := perform(app, http.MethodGet, "/images/"+test.file)
			if response.Code != 200 || response.Header().Get("Content-Type") != test.mime {
				t.Fatalf("изображение: status=%d, MIME=%q", response.Code, response.Header().Get("Content-Type"))
			}
			if !bytes.Equal(response.Body.Bytes(), want) || !bytes.HasPrefix(response.Body.Bytes(), test.signature) {
				t.Fatal("ответ содержит другие байты или неправильную сигнатуру изображения")
			}
			if response.Header().Get("Cache-Control") != "public, max-age=86400" {
				t.Fatalf("локальная фотография не имеет ожидаемого кеширования: %v", response.Header())
			}
			head := perform(app, http.MethodHead, "/images/"+test.file)
			if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Type") != test.mime || head.Header().Get("Content-Length") != strconv.Itoa(len(want)) {
				t.Fatalf("HEAD изображения: status=%d, body=%d, headers=%v", head.Code, head.Body.Len(), head.Header())
			}
		})
	}
}

func TestImageRoutesRejectMissingAndTraversalPaths(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	for _, target := range []string{
		"/images/", "/images/999.jpg", "/images/1.png", "/images/images.json",
		"/images/../data/artists.json", "/images/%2e%2e/1.jpg", "/images/..%2f1.jpg",
		"/images/..%5c1.jpg", "/images/sub/1.jpg", "/images/%252e%252e%252f1.jpg",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			response := perform(app, method, target)
			if response.Code != 404 || (method == http.MethodHead && response.Body.Len() != 0) {
				t.Errorf("%s %s: status=%d body=%d", method, target, response.Code, response.Body.Len())
			}
		}
	}
	if response := perform(app, http.MethodPost, "/images/1.jpg"); response.Code != 405 || response.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("метод изображения: status=%d Allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func TestRealSeedDetailShowsMatchingPhotoAttribution(t *testing.T) {
	artists, _, err := seed.Load()
	if err != nil {
		t.Fatal(err)
	}
	app, _ := newTestApp(t, artists)
	for _, id := range []int{1, 7} {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			credit, exists := seed.ImageCredit(id)
			if !exists {
				t.Fatal("для проверяемой фотографии отсутствует атрибуция")
			}
			response := perform(app, http.MethodGet, "/artists/"+strconv.Itoa(id))
			body := response.Body.String()
			for _, text := range []string{"Иллюстрация из Wikimedia Commons", html.EscapeString(credit.Author), html.EscapeString(credit.Title), html.EscapeString(credit.License)} {
				if response.Code != 200 || !strings.Contains(body, text) {
					t.Errorf("подробная страница не содержит атрибуцию %q, status=%d", text, response.Code)
				}
			}
			if !hasHTMLLink(body, credit.Source) || (credit.LicenseURL != "" && !hasHTMLLink(body, credit.LicenseURL)) {
				t.Error("на странице нет ссылки на Commons или требуемую лицензию")
			}
			var api artistView
			jsonResponse := perform(app, http.MethodGet, "/api/artists/"+strconv.Itoa(id))
			if err := json.Unmarshal(jsonResponse.Body.Bytes(), &api); err != nil {
				t.Fatal(err)
			}
			if api.ImageCredit == nil || *api.ImageCredit != credit || !strings.HasPrefix(api.Image, "/images/") || !strings.Contains(body, `src="`+api.Image+`"`) {
				t.Fatalf("подпись и локальная фотография не соответствуют JSON: %+v", api)
			}
		})
	}
}

// Шаблонизатор может кодировать символы URL: сравниваем смысл ссылки после декодирования.
func hasHTMLLink(body, expected string) bool {
	for _, match := range regexp.MustCompile(`href="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		actual, err := url.PathUnescape(html.UnescapeString(match[1]))
		if err == nil && actual == expected {
			return true
		}
	}
	return false
}

func TestExternalImageDoesNotReceiveLocalCommonsCredit(t *testing.T) {
	app, _ := newTestApp(t, auditArtists())
	response := perform(app, http.MethodGet, "/artists/1")
	if response.Code != 200 || strings.Contains(response.Body.String(), "Иллюстрация из Wikimedia Commons") || strings.Contains(response.Body.String(), `id="photo-credit-title"`) {
		t.Fatal("внешней фотографии API ошибочно приписана локальная атрибуция Commons")
	}
	jsonResponse := perform(app, http.MethodGet, "/api/artists/1")
	var artist artistView
	if err := json.Unmarshal(jsonResponse.Body.Bytes(), &artist); err != nil {
		t.Fatal(err)
	}
	if artist.ImageCredit != nil || artist.Image != "https://example.org/queen.jpg" {
		t.Fatalf("внешняя фотография подменена или получила ложную атрибуцию: %+v", artist)
	}
}
