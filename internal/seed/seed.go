// Package seed хранит проверяемый исторический снимок на случай отказа API.
// Он не обращается к сети и не выдаёт архив за свежую концертную афишу.
package seed

import (
	"embed"
	"fmt"
	"time"

	"tour-track/internal/catalog"
)

// SourceCommit указывает неизменяемую версию публичного архива.
// Подробные ссылки, контрольные суммы и ограничения описаны в PROVENANCE.md.
const SourceCommit = "e5029bd6be53db8b0c1a96f3315dec6360a3e6e3"

//go:embed data/*.json
var files embed.FS

// Load возвращает полный архив через общую проверку четырёх частей каталога.
// Время — дата коммита архива, а не неизвестная дата получения исходного API.
// Каждый вызов разбирает JSON заново: вызывающий код не разделяет изменяемые срезы.
func Load() ([]catalog.Artist, time.Time, error) {
	parts := make([][]byte, 0, 4)
	for _, part := range []string{"artists", "locations", "dates", "relation"} {
		body, err := files.ReadFile("data/" + part + ".json")
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("чтение резервного снимка %s: %w", part, err)
		}
		parts = append(parts, body)
	}
	artists, err := catalog.DecodeParts(parts[0], parts[1], parts[2], parts[3])
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("резервный снимок не прошёл проверку: %w", err)
	}
	images, err := imageRecords()
	if err != nil {
		return nil, time.Time{}, err
	}
	// Архивные JSON остаются побайтово неизменными. Только готовая модель для
	// страницы получает локальное фото Commons вместо недоступной ссылки API.
	for index := range artists {
		if image, exists := images[artists[index].ID]; exists {
			artists[index].Image = "/images/" + image.File
		}
	}
	return artists, time.Date(2026, time.October, 29, 1, 59, 18, 0, time.UTC), nil
}
