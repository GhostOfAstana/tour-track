// Package cache хранит целый согласованный каталог и обновляет его одним работником.
package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"tour-track/internal/catalog"
)

// Fetcher позволяет проверять кеш без настоящего интернета.
type Fetcher interface {
	Fetch(context.Context) ([]catalog.Artist, error)
}

type Snapshot struct {
	Artists   []catalog.Artist
	UpdatedAt time.Time
	Stale     bool
}

type Status struct {
	Refreshing bool      `json:"refreshing"`
	LastError  string    `json:"last_error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	Count      int       `json:"count"`
	Stale      bool      `json:"stale"`
}

type Store struct {
	mu          sync.RWMutex
	source      Fetcher
	snapshot    Snapshot
	refreshing  bool
	lastError   string
	nextRefresh time.Time
	queue       chan struct{}
	cooldown    time.Duration
	timeout     time.Duration
	now         func() time.Time
}

// New создаёт кеш; seed — проверенный резерв, а не ответ работающего API.
func New(source Fetcher, seed []catalog.Artist, seedTime time.Time) *Store {
	return &Store{source: source, snapshot: Snapshot{Artists: clone(seed), UpdatedAt: seedTime, Stale: true}, queue: make(chan struct{}, 1), cooldown: 10 * time.Second, timeout: 12 * time.Second, now: time.Now}
}

// RequestRefresh не создаёт горутину на каждый клик и объединяет повторные запросы.
func (s *Store) RequestRefresh() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refreshing {
		return true
	}
	if s.now().Before(s.nextRefresh) {
		return false
	}
	s.refreshing = true
	s.queue <- struct{}{}
	return true
}

// Run вызывается один раз в отдельной горутине; контекст завершает и таймер, и HTTP-запросы.
func (s *Store) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RequestRefresh()
		case <-s.queue:
			s.refresh(ctx)
		}
	}
}

func (s *Store) refresh(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	// Ошибка источника не заменяет последний полный каталог неполными данными.
	artists, err := safeFetch(ctx, s.source)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshing = false
	s.nextRefresh = s.now().Add(s.cooldown)
	if err != nil || len(artists) == 0 {
		s.lastError = "Не удалось обновить данные API. Сохранён предыдущий полный каталог."
		s.snapshot.Stale = true
		return
	}
	s.snapshot = Snapshot{Artists: clone(artists), UpdatedAt: s.now()}
	s.lastError = ""
}

func safeFetch(ctx context.Context, source Fetcher) (artists []catalog.Artist, err error) {
	defer func() {
		if recover() != nil {
			artists = nil
			err = errors.New("сбой источника данных")
		}
	}()
	return source.Fetch(ctx)
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := s.snapshot
	result.Artists = clone(result.Artists)
	return result
}

func (s *Store) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Status{Refreshing: s.refreshing, LastError: s.lastError, UpdatedAt: s.snapshot.UpdatedAt, Count: len(s.snapshot.Artists), Stale: s.snapshot.Stale}
}

// Копии срезов защищают внутренний кеш от случайного изменения обработчиками.
func clone(input []catalog.Artist) []catalog.Artist {
	result := append([]catalog.Artist(nil), input...)
	for i := range result {
		result[i].Members = append([]string(nil), input[i].Members...)
		result[i].Locations = append([]string(nil), input[i].Locations...)
		result[i].Dates = append([]string(nil), input[i].Dates...)
		result[i].Concerts = append([]catalog.Concert(nil), input[i].Concerts...)
		for j := range result[i].Concerts {
			result[i].Concerts[j].Dates = append([]string(nil), input[i].Concerts[j].Dates...)
		}
	}
	return result
}
