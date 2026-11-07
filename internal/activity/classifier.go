// Package activity оценивает концертную активность по открытым правилам из ТЗ.
// Здесь нет обучения модели или скрытых весов: любой результат можно проверить вручную.
package activity

import (
	"math/big"
	"sort"
	"strings"
	"time"
)

const maxInt = int(^uint(0) >> 1)

// Result хранит не только итог, но и исходные числа для объяснения результата.
type Result struct {
	Label          string   `json:"label"`
	Score          int      `json:"score"`
	TourSpread     int      `json:"tour_spread"`
	ConcertCount   int      `json:"concert_count"`
	CareerYears    int      `json:"career_years"`
	RecentConcerts int      `json:"recent_concerts"`
	RisingStar     bool     `json:"rising_star"`
	Countries      []string `json:"countries"`
	InvalidDates   int      `json:"invalid_dates"`
}

// Score вычисляет 2 × страны + 3 × концерты + min(10, годы / 5).
// Деление целочисленное: неполные пятилетия не дают дополнительного балла.
// Отрицательные значения считаются нулём. Переполнение насыщается максимумом int.
func Score(tourSpread, concertCount, careerYears int) int {
	countries := positive(tourSpread)
	concerts := positive(concertCount)
	careerBonus := min(10, positive(careerYears)/5)
	return add(add(multiply(countries, 2), multiply(concerts, 3)), careerBonus)
}

// Classify возвращает одну из четырёх меток с точными границами из ТЗ.
func Classify(tourSpread, concertCount, careerYears int) string {
	return label(Score(tourSpread, concertCount, careerYears))
}

func label(score int) string {
	switch {
	case score <= 10:
		return "Emerging"
	case score <= 25:
		return "Active"
	case score <= 40:
		return "Established"
	default:
		return "Legendary"
	}
}

// Countries извлекает страну после последнего дефиса в «город-страна».
// Пустые и неполные записи не создают выдуманную страну.
// Небольшой список синонимов объединяет распространённые обозначения одной страны.
func Countries(locations []string) []string {
	unique := make(map[string]struct{}, len(locations))
	for _, location := range locations {
		location = strings.TrimSpace(location)
		separator := strings.LastIndexByte(location, '-')
		if separator <= 0 || separator == len(location)-1 {
			continue
		}
		country := strings.ToLower(strings.TrimSpace(location[separator+1:]))
		country = strings.Join(strings.Fields(strings.ReplaceAll(country, "_", " ")), " ")
		switch country {
		case "usa", "united states", "united states of america":
			country = "usa"
		case "uk", "united kingdom":
			country = "uk"
		case "brasil", "brazil":
			country = "brazil"
		}
		if country != "" {
			unique[country] = struct{}{}
		}
	}
	countries := make([]string, 0, len(unique))
	for country := range unique {
		countries = append(countries, country)
	}
	sort.Strings(countries)
	return countries
}

// IsRisingStar сравнивает среднее за два года со средним за всю карьеру.
// Строгое условие recent / 2 > (total / years) × 1,5 равносильно
// recent × years > total × 3. big.Int сохраняет точность даже на границе int.
// При нулевом стаже среднего за карьеру ещё нет, поэтому метка не выдаётся.
func IsRisingStar(concertCount, recentConcerts, careerYears int) bool {
	if concertCount <= 0 || recentConcerts <= 0 || careerYears <= 0 {
		return false
	}
	recent := new(big.Int).Mul(big.NewInt(int64(recentConcerts)), big.NewInt(int64(careerYears)))
	average := new(big.Int).Mul(big.NewInt(int64(concertCount)), big.NewInt(3))
	return recent.Cmp(average) > 0
}

// Analyze применяет правила к данным артиста на момент конкретного запроса.
// Каждая запись dates считается концертом, включая повторные даты: в один день
// могут проходить разные выступления. Некорректные даты входят в общий счётчик
// записей API, но не в недавние концерты; их число видно в InvalidDates.
func Analyze(locations []string, dates []string, creationYear int, now time.Time) Result {
	countries := Countries(locations)
	careerYears := difference(now.Year(), creationYear)
	result := Result{
		TourSpread:   len(countries),
		ConcertCount: len(dates),
		CareerYears:  careerYears,
		Countries:    countries,
	}
	// Сравниваются календарные даты в часовом поясе запроса. Концерты сегодня
	// включаются целиком; будущие даты не показывают уже состоявшееся ускорение.
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	windowStart := today.AddDate(-2, 0, 0)
	for _, raw := range dates {
		value := strings.TrimSpace(raw)
		value = strings.TrimPrefix(value, "*")
		date, err := time.ParseInLocation("02-01-2006", value, now.Location())
		if err != nil {
			result.InvalidDates++
			continue
		}
		if !date.Before(windowStart) && !date.After(today) {
			result.RecentConcerts++
		}
	}
	result.Score = Score(result.TourSpread, result.ConcertCount, result.CareerYears)
	result.Label = label(result.Score)
	result.RisingStar = IsRisingStar(result.ConcertCount, result.RecentConcerts, result.CareerYears)
	return result
}

func positive(value int) int {
	return max(0, value)
}

func multiply(value, weight int) int {
	if value > maxInt/weight {
		return maxInt
	}
	return value * weight
}

func add(left, right int) int {
	if left > maxInt-right {
		return maxInt
	}
	return left + right
}

func difference(year, creationYear int) int {
	if creationYear >= year {
		return 0
	}
	if creationYear < 0 && year > maxInt+creationYear {
		return maxInt
	}
	return year - creationYear
}
