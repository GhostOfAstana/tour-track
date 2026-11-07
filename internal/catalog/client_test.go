package catalog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture() map[string]string {
	return map[string]string{
		"/artists":   `[{"id":7,"name":"Queen","image":"https://example.org/queen.jpg","members":["Freddie Mercury","John Daecon"],"creationDate":1970,"firstAlbum":"13-07-1973","locations":"https://untrusted.invalid/never-follow"}]`,
		"/locations": `{"index":[{"id":7,"locations":["london-uk","boston-usa"],"dates":"https://untrusted.invalid/never-follow"}]}`,
		"/dates":     `{"index":[{"id":7,"dates":["*01-01-2025","02-01-2025","02-01-2025"]}]}`,
		"/relation":  `{"index":[{"id":7,"datesLocations":{"london-uk":["02-01-2025","02-01-2025"],"boston-usa":["*01-01-2025"]}}]}`,
	}
}

func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestFetchJoinsAllFourParts(t *testing.T) {
	data := fixture()
	var mu sync.Mutex
	calls := make(map[string]int)
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/json" {
			t.Errorf("неверный запрос: %s, Accept=%s", r.Method, r.Header.Get("Accept"))
		}
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		body, exists := data[r.URL.Path]
		if !exists {
			t.Errorf("неожиданный адрес: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	artists, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) != 1 {
		t.Fatalf("число исполнителей: %d", len(artists))
	}
	artist := artists[0]
	if artist.ID != 7 || artist.Name != "Queen" || artist.FirstAlbum != "13-07-1973" || artist.CreationDate != 1970 || artist.Image != "https://example.org/queen.jpg" {
		t.Fatalf("потеряны исходные поля: %+v", artist)
	}
	if !reflect.DeepEqual(artist.Members, []string{"Freddie Mercury", "John Daecon"}) || !reflect.DeepEqual(artist.Locations, []string{"london-uk", "boston-usa"}) || !reflect.DeepEqual(artist.Dates, []string{"*01-01-2025", "02-01-2025", "02-01-2025"}) {
		t.Fatalf("изменены исходные строки или дубликаты: %+v", artist)
	}
	wantConcerts := []Concert{{Location: "boston-usa", Dates: []string{"*01-01-2025"}}, {Location: "london-uk", Dates: []string{"02-01-2025", "02-01-2025"}}}
	if !reflect.DeepEqual(artist.Concerts, wantConcerts) {
		t.Fatalf("связи мест и дат: %+v", artist.Concerts)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 4 {
		t.Fatalf("ожидались четыре адреса, получено: %+v", calls)
	}
	for path, count := range calls {
		if count != 1 {
			t.Errorf("%s запрошен %d раз", path, count)
		}
	}
}

func TestFetchRunsRequestsInParallel(t *testing.T) {
	data := fixture()
	var mu sync.Mutex
	arrived := 0
	allArrived := make(chan struct{})
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		arrived++
		if arrived == 4 {
			close(allArrived)
		}
		mu.Unlock()
		select {
		case <-allArrived:
			fmt.Fprint(w, data[r.URL.Path])
		case <-r.Context().Done():
		}
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.Fetch(ctx); err != nil {
		t.Fatalf("все четыре запроса должны дойти до барьера одновременно: %v", err)
	}
}

func TestFetchRejectsBrokenSnapshots(t *testing.T) {
	tests := []struct {
		name string
		part string
		body string
	}{
		{"сломанный JSON", "/artists", `[{`},
		{"второй документ", "/artists", fixture()["/artists"] + `{}`},
		{"мусор после документа", "/dates", fixture()["/dates"] + ` trailing`},
		{"пустой каталог", "/artists", `[]`},
		{"null вместо каталога", "/artists", `null`},
		{"нулевой id", "/artists", `[{"id":0,"name":"Queen"}]`},
		{"пустое имя", "/artists", `[{"id":7,"name":"  "}]`},
		{"дубликат исполнителя", "/artists", `[{"id":7,"name":"One"},{"id":7,"name":"Two"}]`},
		{"неизвестный id", "/dates", `{"index":[{"id":8,"dates":[]}]}`},
		{"дубликат locations", "/locations", `{"index":[{"id":7,"locations":[]},{"id":7,"locations":[]}]}`},
		{"дубликат dates", "/dates", `{"index":[{"id":7,"dates":[]},{"id":7,"dates":[]}]}`},
		{"дубликат relation", "/relation", `{"index":[{"id":7,"datesLocations":{}},{"id":7,"datesLocations":{}}]}`},
		{"пропущены locations", "/locations", `{"index":[]}`},
		{"пропущены dates", "/dates", `{"index":[]}`},
		{"пропущены relation", "/relation", `{"index":[]}`},
		{"null locations", "/locations", `{"index":[{"id":7,"locations":null}]}`},
		{"null dates", "/dates", `{"index":[{"id":7,"dates":null}]}`},
		{"null relation", "/relation", `{"index":[{"id":7,"datesLocations":null}]}`},
		{"null даты места", "/relation", `{"index":[{"id":7,"datesLocations":{"london-uk":null}}]}`},
		{"слишком большой ответ", "/dates", strings.Repeat(" ", maxResponseBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := fixture()
			data[test.part] = test.body
			client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, data[r.URL.Path])
			}))
			artists, err := client.Fetch(context.Background())
			if err == nil || artists != nil {
				t.Fatalf("повреждённый снимок должен быть отвергнут целиком: artists=%+v, err=%v", artists, err)
			}
		})
	}
}

func TestFetchAllowsArtistWithoutConcerts(t *testing.T) {
	data := fixture()
	data["/locations"] = `{"index":[{"id":7,"locations":[]}]}`
	data["/dates"] = `{"index":[{"id":7,"dates":[]}]}`
	data["/relation"] = `{"index":[{"id":7,"datesLocations":{}}]}`
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, data[r.URL.Path]) }))
	artists, err := client.Fetch(context.Background())
	if err != nil || len(artists) != 1 || len(artists[0].Dates) != 0 || len(artists[0].Concerts) != 0 {
		t.Fatalf("корректный исполнитель без концертов не должен теряться: %+v, %v", artists, err)
	}
}

func TestFetchRejectsHTTPError(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	if artists, err := client.Fetch(context.Background()); err == nil || artists != nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("HTTP 503 должен стать ошибкой без частичного результата: %+v, %v", artists, err)
	}
}

func TestFetchCancellation(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if artists, err := client.Fetch(ctx); err == nil || artists != nil {
		t.Fatalf("отменённый запрос должен завершиться ошибкой: %+v, %v", artists, err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("отмена запроса не сработала своевременно")
	}
}

func TestFetchTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	httpClient := server.Client()
	httpClient.Timeout = 30 * time.Millisecond
	client, err := NewClient(server.URL, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if artists, err := client.Fetch(context.Background()); err == nil || artists != nil {
		t.Fatalf("тайм-аут должен стать ошибкой: %+v, %v", artists, err)
	}
}

func TestNewClientURLValidation(t *testing.T) {
	for _, address := range []string{"", "://", "file:///etc/passwd", "ftp://example.org", "https:///missing-host", "https://user:secret@example.org", "https://example.org?key=value", "https://example.org#fragment"} {
		if _, err := NewClient(address, nil); err == nil {
			t.Errorf("принят неверный адрес: %q", address)
		}
	}
	original := &http.Client{}
	client, err := NewClient("https://example.org/api/", original)
	if err != nil {
		t.Fatal(err)
	}
	if original.Timeout != 0 || client.http.Timeout <= 0 || client.baseURL != "https://example.org/api" {
		t.Fatalf("неверные настройки клиента: original=%+v, client=%+v", original, client)
	}
}

func TestFetchConcurrentCalls(t *testing.T) {
	data := fixture()
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, data[r.URL.Path]) }))
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			artists, err := client.Fetch(context.Background())
			if err != nil || len(artists) != 1 {
				t.Errorf("параллельная загрузка: %+v, %v", artists, err)
			}
		}()
	}
	wg.Wait()
}
