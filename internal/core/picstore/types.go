package picstore

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type PicImage struct {
	ID        uint
	Filename  string
	Path      string // полный путь хранения
	Extension string
	IsPublic  bool
	UserID    uint
	Tags      string
	Data      []byte
	Next      *PicImage
	Prev      *PicImage
}

type picImages []*PicImage

func (p picImages) MarshalBinary() ([]byte, error) {
	return json.Marshal(p)
}

func (p *picImages) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, p)
}

func (p *PicImage) GetData() []byte {
	res := []byte{}
	f, err := os.Open(p.Path)
	if err != nil {
		return res
	}

	res, err = io.ReadAll(f)
	if err != nil {
		return res
	}

	p.Data = res
	return p.Data
}

func extensify(e string) string {
	e = strings.ReplaceAll(e, ".", "")
	e = strings.ToLower(e)

	return e
}

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
