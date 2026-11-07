package seed_test

import (
	"reflect"
	"testing"
	"time"

	"tour-track/internal/activity"
	"tour-track/internal/catalog"
	"tour-track/internal/seed"
)

func TestSnapshotContainsEveryAuditExample(t *testing.T) {
	artists, timestamp, err := seed.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) != 52 {
		t.Fatalf("архив содержит %d артистов вместо 52", len(artists))
	}
	if !timestamp.Equal(time.Date(2026, time.October, 29, 1, 59, 18, 0, time.UTC)) {
		t.Fatalf("время должно соответствовать коммиту источника: %v", timestamp)
	}
	byName := make(map[string]catalog.Artist, len(artists))
	for position, artist := range artists {
		if artist.ID != position+1 {
			t.Fatalf("потерян исходный id: позиция %d, id %d", position, artist.ID)
		}
		if artist.Locations == nil || artist.Dates == nil || artist.Concerts == nil {
			t.Fatalf("потеряна одна из четырёх частей у %s", artist.Name)
		}
		byName[artist.Name] = artist
	}
	wantQueen := []string{"Freddie Mercury", "Brian May", "John Daecon", "Roger Meddows-Taylor", "Mike Grose", "Barry Mitchell", "Doug Fogie"}
	if got := byName["Queen"].Members; !reflect.DeepEqual(got, wantQueen) {
		t.Errorf("состав Queen изменён: %v", got)
	}
	wantFoo := []string{"Dave Grohl", "Nate Mendel", "Taylor Hawkins", "Chris Shiflett", "Pat Smear", "Rami Jaffee"}
	if got := byName["Foo Fighters"].Members; !reflect.DeepEqual(got, wantFoo) {
		t.Errorf("состав Foo Fighters изменён: %v", got)
	}
	if got := byName["Gorillaz"].FirstAlbum; got != "26-03-2001" {
		t.Errorf("дата первого альбома Gorillaz изменена: %q", got)
	}
	wantTravis := []string{"santiago-chile", "sao_paulo-brazil", "los_angeles-usa", "houston-usa", "atlanta-usa", "new_orleans-usa", "philadelphia-usa", "london-uk", "frauenfeld-switzerland", "turku-finland"}
	travis := byName["Travis Scott"]
	if !reflect.DeepEqual(travis.Locations, wantTravis) {
		t.Errorf("локации Travis Scott изменены: %v", travis.Locations)
	}
	result := activity.Analyze(travis.Locations, travis.Dates, travis.CreationDate, time.Date(2026, time.November, 7, 0, 0, 0, 0, time.UTC))
	if result.Label != "Established" && result.Label != "Legendary" {
		t.Errorf("много концертов Travis Scott не должно давать Emerging: %+v", result)
	}
}

func TestLoadReturnsIndependentSnapshots(t *testing.T) {
	first, _, err := seed.Load()
	if err != nil {
		t.Fatal(err)
	}
	first[0].Members[0] = "изменено"
	first[0].Dates[0] = "изменено"
	first[0].Concerts[0].Dates[0] = "изменено"
	second, _, err := seed.Load()
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Members[0] == "изменено" || second[0].Dates[0] == "изменено" || second[0].Concerts[0].Dates[0] == "изменено" {
		t.Fatal("вызовы Load разделяют изменяемые данные")
	}
}
