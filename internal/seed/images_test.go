package seed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"strings"
	"testing"
)

func TestAllImagesAreLocalValidAndAttributed(t *testing.T) {
	var manifest []struct {
		ID          int    `json:"id"`
		File        string `json:"file"`
		SHA256      string `json:"sha256"`
		ContentType string `json:"content_type"`
	}
	if err := json.Unmarshal(imageMetadata, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest) != 52 {
		t.Fatalf("ожидались 52 изображения, получено %d", len(manifest))
	}
	entries, err := fs.ReadDir(Images(), ".")
	if err != nil || len(entries) != len(manifest) {
		t.Fatalf("каталог фотографий не совпадает с манифестом: %d, %v", len(entries), err)
	}
	totalBytes := 0
	for _, record := range manifest {
		body, err := fs.ReadFile(Images(), record.File)
		if err != nil {
			t.Fatal(err)
		}
		totalBytes += len(body)
		checksum := sha256.Sum256(body)
		if hex.EncodeToString(checksum[:]) != record.SHA256 {
			t.Errorf("изображение %s не совпадает с сохранённым источником", record.File)
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 2048 || config.Height > 2048 {
			t.Errorf("некорректная фотография %s: %+v, %v", record.File, config, err)
		}
		if (format == "jpeg" && record.ContentType != "image/jpeg") || (format == "png" && record.ContentType != "image/png") {
			t.Errorf("неверный MIME фотографии %s: %s", record.File, record.ContentType)
		}
		credit, exists := ImageCredit(record.ID)
		if !exists || credit.Author == "" || credit.Title == "" || !strings.HasPrefix(credit.Source, "https://commons.wikimedia.org/wiki/File:") {
			t.Errorf("у фотографии %s нет проверяемой атрибуции: %+v", record.File, credit)
		}
		if !(strings.HasPrefix(credit.License, "CC BY") || credit.License == "Public domain" || credit.License == "CC0" || credit.License == "Copyrighted free use") {
			t.Errorf("недопустимая лицензия %s: %q", record.File, credit.License)
		}
	}
	if totalBytes > 8<<20 {
		t.Errorf("локальные миниатюры занимают больше 8 MiB: %d байт", totalBytes)
	}
	if _, exists := ImageCredit(999); exists {
		t.Error("несуществующий артист не должен получать чужую атрибуцию")
	}
}

func TestImageFileSystemDoesNotExposeOtherFiles(t *testing.T) {
	for _, path := range []string{"../data/artists.json", "data/artists.json", "images.json", "/1.jpg", "1.jpg/../images.json"} {
		file, err := Images().Open(path)
		if err == nil {
			file.Close()
			t.Errorf("служебный или некорректный путь %q стал доступен как фотография", path)
		}
	}
}
