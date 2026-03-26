package picstore

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// PicImage представляет метаданные изображения в системе.
// Используется для передачи данных между слоями приложения.
type PicImage struct {
	ID          uint
	Filename    string
	Path        string `json:"path"` // относительный путь хранения (для шаблонов и JSON)
	FullPath    string `json:"-"`    // полный путь к файлу на диске (для внутреннего использования)
	PreviewPath string // путь к превью (относительный)
	Extension   string
	IsPublic    bool
	UserID      uint
	Tags        string
	Views       uint      // количество просмотров
	IsEncrypted bool      `json:"-"` // не сериализуется в JSON
	Salt        []byte    `json:"-"` // соль для шифрования
	Nonce       []byte    `json:"-"` // одноразовый номер для шифрования
	Data        []byte    `json:"-"` // бинарные данные изображения (загружаются по требованию)
	Next        *PicImage `json:"-"` // ссылка на следующее изображение в навигации
	Prev        *PicImage `json:"-"` // ссылка на предыдущее изображение в навигации
}

// picImages — срез PicImage с поддержкой сериализации в Redis.
type picImages []*PicImage

// MarshalBinary реализует encoding.BinaryMarshaler для сериализации в Redis.
func (p picImages) MarshalBinary() ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalBinary реализует encoding.BinaryUnmarshaler для десериализации из Redis.
// После десериализации восстанавливает связи Prev/Next между изображениями.
func (p *picImages) UnmarshalBinary(data []byte) error {
	if err := json.Unmarshal(data, p); err != nil {
		return err
	}
	p.RestoreLinks()
	return nil
}

// RestoreLinks восстанавливает двунаправленные связи Prev/Next для всех изображений в срезе.
// Вызывается после десериализации из кэша.
func (p *picImages) RestoreLinks() {
	for i := range *p {
		if i > 0 {
			(*p)[i].Prev = (*p)[i-1]
		} else {
			(*p)[i].Prev = nil
		}
		if i < len(*p)-1 {
			(*p)[i].Next = (*p)[i+1]
		} else {
			(*p)[i].Next = nil
		}
	}
}

// GetData загружает бинарные данные изображения с диска.
// Если данные уже загружены, возвращает их из поля Data.
// В случае ошибки чтения файла возвращает пустой срез.
func (p *PicImage) GetData() []byte {
	res := []byte{}
	path := p.FullPath
	if path == "" {
		// для обратной совместимости используем Path (ожидается полный путь)
		path = p.Path
	}
	f, err := os.Open(path)
	if err != nil {
		return res
	}
	defer f.Close()

	res, err = io.ReadAll(f)
	if err != nil {
		return res
	}

	p.Data = res
	return p.Data
}

// extensify нормализует расширение файла: удаляет точку и приводит к нижнему регистру.
// Пример: ".JPG" -> "jpg".
func extensify(e string) string {
	e = strings.ReplaceAll(e, ".", "")
	e = strings.ToLower(e)

	return e
}

// downloadImage загружает изображение по URL и возвращает его бинарные данные.
// В случае ошибки сети или не‑успешного HTTP‑статуса возвращает ошибку.
func downloadImage(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed getting data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed load data: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed read body: %w", err)
	}

	return data, nil
}

// NavigationContext представляет окно изображений вокруг текущего для навигации.
// Содержит текущее изображение, предыдущие и следующие изображения в пределах окна,
// а также общее количество изображений в текущем контексте (например, в фильтре).
type NavigationContext struct {
	Current  *PicImage   `json:"current"`             // текущее изображение
	Prev     []*PicImage `json:"prev,omitempty"`      // предыдущие изображения (ближайшие первыми)
	Next     []*PicImage `json:"next,omitempty"`      // следующие изображения (ближайшие первыми)
	Total    int         `json:"total"`               // общее количество изображений в текущем контексте (фильтр + пользователь)
	HasPrev  bool        `json:"has_prev"`            // есть ли предыдущее изображение за пределами окна
	HasNext  bool        `json:"has_next"`            // есть ли следующее изображение за пределами окна
	Window   int         `json:"window"`              // размер окна (сколько изображений в каждую сторону)
	Filter   string      `json:"filter,omitempty"`    // применённый фильтр тегов (строка)
	UserID   uint        `json:"user_id,omitempty"`   // ID пользователя (0 для публичных)
	IsPublic bool        `json:"is_public,omitempty"` // флаг публичного контекста
}
