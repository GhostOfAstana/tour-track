package seed

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"sync"
)

// Credit содержит готовую текстовую атрибуцию фотографии для HTML-шаблона.
// Поля не содержат доверенного HTML; шаблон экранирует их как обычный текст.
type Credit struct {
	Artist, Title, Author, License, Source, LicenseURL string
}

type imageRecord struct {
	ID         int    `json:"id"`
	Artist     string `json:"artist"`
	File       string `json:"file"`
	Title      string `json:"commons_title"`
	Author     string `json:"author"`
	License    string `json:"license"`
	Source     string `json:"source_page"`
	LicenseURL string `json:"license_url"`
}

//go:embed images/*
var imageFiles embed.FS

//go:embed images.json
var imageMetadata []byte

var (
	metadataOnce sync.Once
	metadataByID map[int]imageRecord
	metadataErr  error
)

func imageRecords() (map[int]imageRecord, error) {
	metadataOnce.Do(func() {
		var records []imageRecord
		if err := json.Unmarshal(imageMetadata, &records); err != nil {
			metadataErr = fmt.Errorf("неверные сведения об изображениях: %w", err)
			return
		}
		metadataByID = make(map[int]imageRecord, len(records))
		for _, record := range records {
			prefix := strconv.Itoa(record.ID)
			validFile := record.File == prefix+".jpg" || record.File == prefix+".png" || record.File == prefix+".webp"
			if record.ID <= 0 || !validFile || record.Author == "" || record.License == "" || !strings.HasPrefix(record.Source, "https://commons.wikimedia.org/wiki/File:") {
				metadataErr = fmt.Errorf("неполная атрибуция изображения id %d", record.ID)
				return
			}
			if _, exists := metadataByID[record.ID]; exists {
				metadataErr = fmt.Errorf("повторная атрибуция изображения id %d", record.ID)
				return
			}
			if _, err := imageFiles.ReadFile("images/" + record.File); err != nil {
				metadataErr = fmt.Errorf("отсутствует изображение id %d: %w", record.ID, err)
				return
			}
			metadataByID[record.ID] = record
		}
	})
	return metadataByID, metadataErr
}

// ImageCredit возвращает копию атрибуции, не открывая общую изменяемую map.
func ImageCredit(id int) (Credit, bool) {
	records, err := imageRecords()
	if err != nil {
		return Credit{}, false
	}
	record, exists := records[id]
	if !exists {
		return Credit{}, false
	}
	return Credit{
		Artist: record.Artist, Title: strings.TrimPrefix(record.Title, "File:"),
		Author: record.Author, License: record.License, Source: record.Source, LicenseURL: record.LicenseURL,
	}, true
}

// Images открывает только каталог локальных фотографий: например "1.jpg".
// JSON-архив и служебные файлы через эту файловую систему недоступны.
func Images() fs.FS {
	return imageDirectory{}
}

type imageDirectory struct{}

func (imageDirectory) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		return imageFiles.Open("images")
	}
	return imageFiles.Open("images/" + name)
}
