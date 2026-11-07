package cache

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tour-track/internal/catalog"
)

type fetchFunc func(context.Context) ([]catalog.Artist, error)

func (f fetchFunc) Fetch(ctx context.Context) ([]catalog.Artist, error) { return f(ctx) }

func sampleArtists() []catalog.Artist {
	return []catalog.Artist{{
		ID: 1, Name: "Queen", Members: []string{"Freddie Mercury"},
		Locations: []string{"london-uk"}, Dates: []string{"01-01-2025"},
		Concerts: []catalog.Concert{{Location: "london-uk", Dates: []string{"01-01-2025"}}},
	}}
}

// Ожидание ограничено сроком: зависший работник не подвесит весь набор тестов.
func waitIdle(t *testing.T, store *Store) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for store.Status().Refreshing {
		select {
		case <-deadline.C:
			t.Fatal("работник не завершил обновление за отведённое время")
		case <-poll.C:
		}
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

func TestConcurrentClicksShareOneRefresh(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	store := New(fetchFunc(func(ctx context.Context) ([]catalog.Artist, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return sampleArtists(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}), nil, time.Time{})

	clickTogether := func() {
		var wg sync.WaitGroup
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if !store.RequestRefresh() {
					t.Error("нажатие должно присоединиться к общему обновлению")
				}
			}()
		}
		wg.Wait()
	}
	clickTogether()
	if len(store.queue) != 1 {
		t.Fatalf("до запуска работника ожидалось одно задание, получено %d", len(store.queue))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	go func() { defer close(stopped); store.Run(ctx, time.Hour) }()
	waitSignal(t, started, "загрузка не началась")
	clickTogether()
	if len(store.queue) != 0 || calls.Load() != 1 {
		t.Fatalf("повторные нажатия создали лишние задания: queue=%d, calls=%d", len(store.queue), calls.Load())
	}
	close(release)
	waitIdle(t, store)
	if status := store.Status(); status.Count != 1 || status.Stale || status.LastError != "" {
		t.Fatalf("успешный результат не опубликован: %+v", status)
	}
	cancel()
	waitSignal(t, stopped, "работник не остановился")
}

func TestFailedRefreshKeepsCompletePreviousCatalog(t *testing.T) {
	loaded := sampleArtists()
	store := New(fetchFunc(func(context.Context) ([]catalog.Artist, error) { return loaded, nil }), nil, time.Time{})
	store.refresh(context.Background())
	before := store.Snapshot()
	store.source = fetchFunc(func(context.Context) ([]catalog.Artist, error) {
		return []catalog.Artist{{ID: 9, Name: "Неполный ответ"}}, errors.New("источник оборвал соединение")
	})
	store.refresh(context.Background())
	after := store.Snapshot()
	if !reflect.DeepEqual(before.Artists, after.Artists) || !before.UpdatedAt.Equal(after.UpdatedAt) || !after.Stale {
		t.Fatalf("ошибка заменила предыдущий каталог: before=%+v, after=%+v", before, after)
	}
	if status := store.Status(); status.Refreshing || status.LastError == "" || status.Count != len(before.Artists) {
		t.Fatalf("состояние ошибки не отражено: %+v", status)
	}
}

func TestFirstRefreshFailureWithoutSeed(t *testing.T) {
	for _, test := range []struct {
		name string
		load fetchFunc
	}{
		{"ошибка источника", func(context.Context) ([]catalog.Artist, error) { return nil, errors.New("отказ") }},
		{"пустой ответ", func(context.Context) ([]catalog.Artist, error) { return []catalog.Artist{}, nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := New(test.load, nil, time.Time{})
			store.refresh(context.Background())
			if snapshot := store.Snapshot(); len(snapshot.Artists) != 0 || !snapshot.Stale || !snapshot.UpdatedAt.IsZero() {
				t.Fatalf("неуспешная загрузка выдала вымышленные данные: %+v", snapshot)
			}
			if status := store.Status(); status.LastError == "" || status.Refreshing {
				t.Fatalf("неуспешное обновление не завершено: %+v", status)
			}
		})
	}
}

func TestFetcherPanicDoesNotDestroyCache(t *testing.T) {
	seed := sampleArtists()
	store := New(fetchFunc(func(context.Context) ([]catalog.Artist, error) { panic("неожиданный сбой") }), seed, time.Now())
	store.refresh(context.Background())
	if !reflect.DeepEqual(store.Snapshot().Artists, seed) || store.Status().LastError == "" || store.Status().Refreshing {
		t.Fatalf("паника источника повредила кеш: %+v", store.Status())
	}
	store.source = fetchFunc(func(context.Context) ([]catalog.Artist, error) { return sampleArtists(), nil })
	store.refresh(context.Background())
	if status := store.Status(); status.LastError != "" || status.Stale {
		t.Fatalf("после паники кеш не смог восстановиться: %+v", status)
	}
}

func corruptArtists(artists []catalog.Artist) {
	artists[0].Name = "Испорчено"
	artists[0].Members[0] = "Испорчено"
	artists[0].Locations[0] = "Испорчено"
	artists[0].Dates[0] = "Испорчено"
	artists[0].Concerts[0].Location = "Испорчено"
	artists[0].Concerts[0].Dates[0] = "Испорчено"
}

func TestCacheCopiesNestedSlices(t *testing.T) {
	seed := sampleArtists()
	store := New(nil, seed, time.Now())
	corruptArtists(seed)
	if got := store.Snapshot().Artists; !reflect.DeepEqual(got, sampleArtists()) {
		t.Fatalf("изменение seed изменило кеш: %+v", got)
	}
	copy := store.Snapshot()
	corruptArtists(copy.Artists)
	if got := store.Snapshot().Artists; !reflect.DeepEqual(got, sampleArtists()) {
		t.Fatalf("изменение выданного снимка изменило кеш: %+v", got)
	}
	loaded := sampleArtists()
	store.source = fetchFunc(func(context.Context) ([]catalog.Artist, error) { return loaded, nil })
	store.refresh(context.Background())
	corruptArtists(loaded)
	if got := store.Snapshot().Artists; !reflect.DeepEqual(got, sampleArtists()) {
		t.Fatalf("изменение результата Fetch изменило кеш: %+v", got)
	}
}

func TestRefreshCooldownBoundary(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	store := New(fetchFunc(func(context.Context) ([]catalog.Artist, error) { return sampleArtists(), nil }), nil, time.Time{})
	store.now = func() time.Time { return now }
	store.cooldown = 10 * time.Second
	store.refresh(context.Background())
	if store.RequestRefresh() {
		t.Fatal("новое обновление разрешено сразу после завершения предыдущего")
	}
	now = now.Add(10*time.Second - time.Nanosecond)
	if store.RequestRefresh() {
		t.Fatal("новое обновление разрешено до окончания паузы")
	}
	now = now.Add(time.Nanosecond)
	if !store.RequestRefresh() || len(store.queue) != 1 {
		t.Fatal("на границе окончания паузы обновление должно быть разрешено")
	}
}

func TestRunCancelsInFlightFetchAndStops(t *testing.T) {
	started := make(chan struct{})
	cancelObserved := make(chan struct{})
	store := New(fetchFunc(func(ctx context.Context) ([]catalog.Artist, error) {
		close(started)
		<-ctx.Done()
		close(cancelObserved)
		return nil, ctx.Err()
	}), sampleArtists(), time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	go func() { defer close(stopped); store.Run(ctx, time.Hour) }()
	if !store.RequestRefresh() {
		t.Fatal("обновление не поставлено в очередь")
	}
	waitSignal(t, started, "загрузка не началась")
	cancel()
	waitSignal(t, cancelObserved, "отмена не дошла до источника")
	waitSignal(t, stopped, "работник не остановился после отмены")
	if status := store.Status(); status.Refreshing || status.LastError == "" || status.Count != 1 {
		t.Fatalf("при остановке потерян каталог или осталось незавершённое обновление: %+v", status)
	}
}
