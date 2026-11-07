package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const maxResponseBytes = 4 << 20

// Client читает только четыре заранее известных адреса API.
// Ссылки внутри полученного JSON никогда не используются для новых запросов.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient принимает адрес API, например https://example.org/api.
// Переданный HTTP-клиент копируется: настройки вызывающей стороны не изменяются.
func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed == nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("адрес API должен быть HTTP(S)-адресом без пароля, параметров и фрагмента")
	}
	client := http.Client{Timeout: 12 * time.Second}
	if httpClient != nil {
		client = *httpClient
		if client.Timeout <= 0 {
			client.Timeout = 12 * time.Second
		}
	}
	return &Client{baseURL: strings.TrimRight(parsed.String(), "/"), http: &client}, nil
}

type artistRecord struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Image        string   `json:"image"`
	Members      []string `json:"members"`
	CreationDate int      `json:"creationDate"`
	FirstAlbum   string   `json:"firstAlbum"`
}

type locationRecord struct {
	ID        int      `json:"id"`
	Locations []string `json:"locations"`
}

type dateRecord struct {
	ID    int      `json:"id"`
	Dates []string `json:"dates"`
}

type relationRecord struct {
	ID             int                 `json:"id"`
	DatesLocations map[string][]string `json:"datesLocations"`
}

type fetchedPart struct {
	part      string
	artists   []artistRecord
	locations []locationRecord
	dates     []dateRecord
	relations []relationRecord
	err       error
}

// Fetch загружает части каталога одновременно через горутины и канал.
// Возвращается только целый согласованный каталог: частичного обновления нет.
func (c *Client) Fetch(ctx context.Context) ([]Artist, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Буфер позволяет каждой горутине завершиться даже после раннего возврата.
	results := make(chan fetchedPart, 4)
	for _, part := range []string{"artists", "locations", "dates", "relation"} {
		go func(part string) {
			result := fetchedPart{part: part}
			switch part {
			case "artists":
				result.err = c.get(ctx, part, &result.artists)
			case "locations":
				var response struct {
					Index []locationRecord `json:"index"`
				}
				result.err = c.get(ctx, part, &response)
				result.locations = response.Index
			case "dates":
				var response struct {
					Index []dateRecord `json:"index"`
				}
				result.err = c.get(ctx, part, &response)
				result.dates = response.Index
			case "relation":
				var response struct {
					Index []relationRecord `json:"index"`
				}
				result.err = c.get(ctx, part, &response)
				result.relations = response.Index
			}
			results <- result
		}(part)
	}

	var combined fetchedPart
	for range 4 {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("загрузка каталога отменена: %w", ctx.Err())
		case result := <-results:
			if result.err != nil {
				return nil, result.err
			}
			switch result.part {
			case "artists":
				combined.artists = result.artists
			case "locations":
				combined.locations = result.locations
			case "dates":
				combined.dates = result.dates
			case "relation":
				combined.relations = result.relations
			}
		}
	}
	return join(combined)
}

func (c *Client) get(ctx context.Context, part string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/"+part, nil)
	if err != nil {
		return fmt.Errorf("создание запроса %s: %w", part, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "tour-track/1.0")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("загрузка %s: %w", part, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("API %s вернул HTTP %d", part, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("чтение %s: %w", part, err)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("ответ %s превышает ограничение %d байт", part, maxResponseBytes)
	}
	return decodeJSON(part, body, target)
}

// DecodeParts проверяет сохранённые ответы тем же способом, что и живой API.
// Вызывающая сторона получает только полный каталог, связанный по id.
func DecodeParts(artistsJSON, locationsJSON, datesJSON, relationJSON []byte) ([]Artist, error) {
	var parts fetchedPart
	var locations struct {
		Index []locationRecord `json:"index"`
	}
	var dates struct {
		Index []dateRecord `json:"index"`
	}
	var relations struct {
		Index []relationRecord `json:"index"`
	}
	for _, input := range []struct {
		part   string
		body   []byte
		target any
	}{
		{"artists", artistsJSON, &parts.artists},
		{"locations", locationsJSON, &locations},
		{"dates", datesJSON, &dates},
		{"relation", relationJSON, &relations},
	} {
		if err := decodeJSON(input.part, input.body, input.target); err != nil {
			return nil, err
		}
	}
	parts.locations, parts.dates, parts.relations = locations.Index, dates.Index, relations.Index
	return join(parts)
}

func decodeJSON(part string, body []byte, target any) error {
	if len(body) > maxResponseBytes {
		return fmt.Errorf("ответ %s превышает ограничение %d байт", part, maxResponseBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("неверный JSON %s: %w", part, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("после JSON %s обнаружены лишние данные", part)
	}
	return nil
}

func join(parts fetchedPart) ([]Artist, error) {
	if len(parts.artists) == 0 {
		return nil, errors.New("API вернул пустой список исполнителей")
	}
	artists := make([]Artist, len(parts.artists))
	positions := make(map[int]int, len(artists))
	for position, record := range parts.artists {
		if record.ID <= 0 || strings.TrimSpace(record.Name) == "" {
			return nil, errors.New("у исполнителя отсутствует положительный id или название")
		}
		if _, exists := positions[record.ID]; exists {
			return nil, fmt.Errorf("повторный id %d в artists", record.ID)
		}
		positions[record.ID] = position
		artists[position] = Artist{
			ID: record.ID, Name: record.Name, Image: record.Image,
			Members: record.Members, CreationDate: record.CreationDate,
			FirstAlbum: record.FirstAlbum,
		}
	}

	seen := make(map[int]bool, len(artists))
	for _, record := range parts.locations {
		position, err := recordPosition(record.ID, "locations", positions, seen)
		if err != nil {
			return nil, err
		}
		if record.Locations == nil {
			return nil, fmt.Errorf("у id %d отсутствует массив locations", record.ID)
		}
		artists[position].Locations = record.Locations
	}
	if len(seen) != len(artists) {
		return nil, errors.New("часть locations содержит не всех исполнителей")
	}
	seen = make(map[int]bool, len(artists))
	for _, record := range parts.dates {
		position, err := recordPosition(record.ID, "dates", positions, seen)
		if err != nil {
			return nil, err
		}
		if record.Dates == nil {
			return nil, fmt.Errorf("у id %d отсутствует массив dates", record.ID)
		}
		artists[position].Dates = record.Dates
	}
	if len(seen) != len(artists) {
		return nil, errors.New("часть dates содержит не всех исполнителей")
	}
	seen = make(map[int]bool, len(artists))
	for _, record := range parts.relations {
		position, err := recordPosition(record.ID, "relation", positions, seen)
		if err != nil {
			return nil, err
		}
		if record.DatesLocations == nil {
			return nil, fmt.Errorf("у id %d отсутствует объект datesLocations", record.ID)
		}
		locations := make([]string, 0, len(record.DatesLocations))
		for location := range record.DatesLocations {
			locations = append(locations, location)
		}
		sort.Strings(locations)
		artists[position].Concerts = make([]Concert, 0, len(locations))
		for _, location := range locations {
			if record.DatesLocations[location] == nil {
				return nil, fmt.Errorf("у id %d для места %q отсутствует массив дат", record.ID, location)
			}
			artists[position].Concerts = append(artists[position].Concerts, Concert{Location: location, Dates: record.DatesLocations[location]})
		}
	}
	if len(seen) != len(artists) {
		return nil, errors.New("часть relation содержит не всех исполнителей")
	}
	return artists, nil
}

func recordPosition(id int, part string, positions map[int]int, seen map[int]bool) (int, error) {
	position, exists := positions[id]
	if !exists {
		return 0, fmt.Errorf("неизвестный id %d в %s", id, part)
	}
	if seen[id] {
		return 0, fmt.Errorf("повторный id %d в %s", id, part)
	}
	seen[id] = true
	return position, nil
}
