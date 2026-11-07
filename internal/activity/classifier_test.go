package activity_test

import (
	"reflect"
	"testing"
	"time"

	"tour-track/internal/activity"
)

func TestScoreAndClassification(t *testing.T) {
	tests := []struct {
		name                       string
		countries, concerts, years int
		score                      int
		label                      string
	}{
		{"пустые данные", 0, 0, 0, 0, "Emerging"},
		{"граница 10", 0, 0, 50, 10, "Emerging"},
		{"граница 11", 1, 3, 0, 11, "Active"},
		{"граница 25", 2, 7, 0, 25, "Active"},
		{"граница 26", 1, 8, 0, 26, "Established"},
		{"пример ТЗ и граница 40", 5, 8, 30, 40, "Established"},
		{"граница 41", 1, 13, 0, 41, "Legendary"},
		{"неполное пятилетие", 0, 0, 4, 0, "Emerging"},
		{"полное пятилетие", 0, 0, 5, 1, "Emerging"},
		{"49 лет", 0, 0, 49, 9, "Emerging"},
		{"ограничение стажа", 0, 0, 500, 10, "Emerging"},
		{"отрицательные данные", -4, -2, -10, 0, "Emerging"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := activity.Score(tt.countries, tt.concerts, tt.years); got != tt.score {
				t.Errorf("Score = %d; нужно %d", got, tt.score)
			}
			if got := activity.Classify(tt.countries, tt.concerts, tt.years); got != tt.label {
				t.Errorf("Classify = %q; нужно %q", got, tt.label)
			}
		})
	}
}

func TestScoreSaturatesInsteadOfOverflowing(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, inputs := range [][3]int{{maxInt, 0, 0}, {0, maxInt, 0}, {maxInt, maxInt, maxInt}} {
		if got := activity.Score(inputs[0], inputs[1], inputs[2]); got != maxInt {
			t.Errorf("Score(%v) = %d; нужно насыщение %d", inputs, got, maxInt)
		}
		if got := activity.Classify(inputs[0], inputs[1], inputs[2]); got != "Legendary" {
			t.Errorf("Classify(%v) = %q", inputs, got)
		}
	}
	minInt := -maxInt - 1
	if got := activity.Score(minInt, minInt, minInt); got != 0 {
		t.Errorf("минимальные числа должны дать 0, получено %d", got)
	}
}

func TestCountries(t *testing.T) {
	locations := []string{
		"los_angeles-usa", "HOUSTON-USA", " a-united_states ", "b-united states of america",
		"london-uk", "a-united_kingdom", "sao_paulo-brasil", "rio-brazil",
		"new-city-new_zealand", "paris-france", "c-france", "a-south_korea",
		"a-north_korea", "", "invalid", "-usa", "city-", "city-   ",
	}
	want := []string{"brazil", "france", "new zealand", "north korea", "south korea", "uk", "usa"}
	if got := activity.Countries(locations); !reflect.DeepEqual(got, want) {
		t.Errorf("страны = %#v; нужно %#v", got, want)
	}
	if got := activity.Countries(nil); got == nil || len(got) != 0 {
		t.Errorf("пустой список должен сериализоваться как [], получено %#v", got)
	}
}

func TestRisingStar(t *testing.T) {
	tests := []struct {
		name                 string
		total, recent, years int
		want                 bool
	}{
		{"пример ТЗ: 7 против 4", 40, 14, 10, true},
		{"пример ТЗ: 5 против 4", 40, 10, 10, false},
		{"ровно 50 процентов", 40, 12, 10, false},
		{"чуть выше 50 процентов", 40, 13, 10, true},
		{"нулевой стаж", 2, 2, 0, false},
		{"нулевые концерты", 0, 0, 10, false},
		{"отрицательный стаж", 2, 2, -2, false},
		{"отрицательное количество", -2, 2, 10, false},
		{"нет недавних концертов", 2, 0, 10, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := activity.IsRisingStar(tt.total, tt.recent, tt.years); got != tt.want {
				t.Errorf("IsRisingStar = %v; нужно %v", got, tt.want)
			}
		})
	}
	maxInt := int(^uint(0) >> 1)
	if !activity.IsRisingStar(maxInt, maxInt, maxInt) {
		t.Error("перемножение максимальных значений не должно менять верный результат")
	}
	if activity.IsRisingStar(maxInt, 1, maxInt) {
		t.Error("1 × maxInt не больше 3 × maxInt")
	}
}

func TestAnalyzeUsesRequestYearAndDateWindow(t *testing.T) {
	zone := time.FixedZone("UTC+5", 5*60*60)
	now := time.Date(2026, time.November, 7, 17, 45, 0, 0, zone)
	dates := []string{
		"*07-11-2024", // Левая граница включается.
		"06-11-2024",  // День до окна не включается.
		"07-11-2026",  // Сегодня включается независимо от времени суток.
		"08-11-2026",  // Завтра ещё не означает состоявшийся концерт.
		" 07-11-2026 ", "31-02-2026", "", "2026-11-07", "29-02-2024",
	}
	got := activity.Analyze([]string{"a-usa", "b-usa", "a-uk"}, dates, 2000, now)
	if got.TourSpread != 2 || got.ConcertCount != 9 || got.CareerYears != 26 {
		t.Errorf("исходные значения неверны: %+v", got)
	}
	if got.RecentConcerts != 3 || got.InvalidDates != 3 {
		t.Errorf("окно или некорректные даты посчитаны неверно: %+v", got)
	}
	if got.Score != 36 || got.Label != "Established" || !got.RisingStar {
		t.Errorf("итог неверен: %+v", got)
	}
	nextYear := now.AddDate(1, 0, 0)
	if next := activity.Analyze(nil, nil, 2000, nextYear); next.CareerYears != 27 {
		t.Errorf("стаж не пересчитан при следующем запросе: %+v", next)
	}
}

func TestAnalyzeLeapYearAndInvalidCareer(t *testing.T) {
	now := time.Date(2024, time.February, 29, 12, 0, 0, 0, time.UTC)
	got := activity.Analyze(nil, []string{"28-02-2022", "01-03-2022", "29-02-2024"}, 2025, now)
	// AddDate использует календарное правило Go: 29.02.2024 минус два года — 01.03.2022.
	if got.RecentConcerts != 2 || got.CareerYears != 0 || got.RisingStar {
		t.Errorf("неверная обработка високосного года или будущего начала карьеры: %+v", got)
	}
	maxInt := int(^uint(0) >> 1)
	got = activity.Analyze(nil, nil, -maxInt-1, now)
	if got.CareerYears != maxInt || got.Score != 10 {
		t.Errorf("экстремальный год вызвал переполнение: %+v", got)
	}
}

func FuzzScoreNeverBecomesNegative(f *testing.F) {
	f.Add(5, 8, 30)
	f.Add(0, 0, 0)
	f.Add(-1, -1, -1)
	f.Add(int(^uint(0)>>1), 1, 1)
	f.Fuzz(func(t *testing.T, countries, concerts, years int) {
		score := activity.Score(countries, concerts, years)
		if score < 0 {
			t.Fatalf("отрицательный балл %d для (%d, %d, %d)", score, countries, concerts, years)
		}
		if countries >= 0 && countries < int(^uint(0)>>1) && activity.Score(countries+1, concerts, years) < score {
			t.Fatal("добавление страны уменьшило активность")
		}
	})
}

func FuzzAnalyzeMalformedInput(f *testing.F) {
	f.Add("los_angeles-usa", "*07-11-2026", 1990)
	f.Add("", "bad date", 2027)
	now := time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, location, date string, creationYear int) {
		result := activity.Analyze([]string{location}, []string{date}, creationYear, now)
		if result.ConcertCount != 1 || result.Score < 0 || result.CareerYears < 0 || result.RecentConcerts > 1 {
			t.Fatalf("нарушены ограничения результата: %+v", result)
		}
	})
}
