package catalog

import (
	"strings"
	"testing"
)

func TestDecodePartsUsesCompleteValidation(t *testing.T) {
	data := fixture()
	decode := func(data map[string]string) ([]Artist, error) {
		return DecodeParts([]byte(data["/artists"]), []byte(data["/locations"]), []byte(data["/dates"]), []byte(data["/relation"]))
	}
	if artists, err := decode(data); err != nil || len(artists) != 1 || artists[0].Name != "Queen" {
		t.Fatalf("валидный сохранённый снимок отвергнут: %v, %v", artists, err)
	}
	for name, mutate := range map[string]func(map[string]string){
		"нет части": func(data map[string]string) { delete(data, "/dates") },
		"мусор":     func(data map[string]string) { data["/locations"] += " trailing" },
		"неизвестная связь": func(data map[string]string) {
			data["/relation"] = `{"index":[{"id":999,"datesLocations":{}}]}`
		},
		"превышен размер": func(data map[string]string) { data["/dates"] = strings.Repeat(" ", maxResponseBytes+1) },
	} {
		t.Run(name, func(t *testing.T) {
			data := fixture()
			mutate(data)
			if artists, err := decode(data); err == nil || artists != nil {
				t.Fatalf("частичный снимок не должен попадать пользователю: %v, %v", artists, err)
			}
		})
	}
}
