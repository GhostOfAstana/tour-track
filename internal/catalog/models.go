// Package catalog загружает и связывает четыре части музыкального каталога.
package catalog

// Artist объединяет исходные сведения об исполнителе и его концертах.
// Названия, состав группы и даты сохраняются в том виде, в котором их вернул API.
type Artist struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	Image        string    `json:"image"`
	Members      []string  `json:"members"`
	CreationDate int       `json:"creationDate"`
	FirstAlbum   string    `json:"firstAlbum"`
	Locations    []string  `json:"locations"`
	Dates        []string  `json:"dates"`
	Concerts     []Concert `json:"concerts"`
}

// Concert показывает, какие даты связаны с конкретным местом выступления.
type Concert struct {
	Location string   `json:"location"`
	Dates    []string `json:"dates"`
}
