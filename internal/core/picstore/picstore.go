package picstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"picstore/internal/adapters/models"
	"picstore/internal/adapters/storage/types"
	"strings"
	"sync"
	"time"

	"github.com/playmixer/secret-keeper/pkg/tools"
	"github.com/playmixer/single-auth/pkg/logger"
	"go.uber.org/zap"
)

const (
	lengthFilename uint = 40

	nsImagesAll   string = "images:all"
	nsPostsPublic string = "posts:public"
)

type cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	GetH(ctx context.Context, key string, obj types.ObjInterface) (err error)
	SetH(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error
}

type store interface {
	NewImage(ctx context.Context, userID uint, path string, isPublic bool, tags string) (*models.Image, error)
	DelImage(ctx context.Context, userID uint, imageID uint) error
	GetImage(ctx context.Context, path string) (*models.Image, error)
	GetImages(ctx context.Context) ([]*models.Image, error)
	UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error
}

type self interface {
	UploadImgFile(ctx context.Context, userID uint, f *multipart.FileHeader, isPublic bool, tags string) (*PicImage, error)
	UploadImgURL(ctx context.Context, userID uint, url string, isPublic bool, tags string) (*PicImage, error)
	GetImg(ctx context.Context, path string) (*PicImage, error)
	GetPosts(ctx context.Context) ([]*PicImage, error)
	GetPostsWithTags(ctx context.Context, tags string) ([]*PicImage, error)
	GetTagsWithCount(ctx context.Context) (map[string]int, error)
	GetUserPosts(ctx context.Context, userID uint) ([]*PicImage, error)
	UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error
	DeleteImage(ctx context.Context, userID uint, imageID uint) error
}

type PicStore struct {
	store       store
	log         *logger.Logger
	cache       cache
	picturePath string
	locker      map[string]*sync.Mutex
}

var (
	_ self = &PicStore{}
)

func New(cfg Config, log *logger.Logger, store store, cache cache) (*PicStore, error) {
	p := &PicStore{
		store:       store,
		log:         log,
		cache:       cache,
		picturePath: cfg.PicPath,
		locker: map[string]*sync.Mutex{
			nsImagesAll:   &sync.Mutex{},
			nsPostsPublic: &sync.Mutex{},
		},
	}

	return p, nil
}

func (p *PicStore) UploadImgFile(ctx context.Context, userID uint, f *multipart.FileHeader, isPublic bool, tags string) (*PicImage, error) {
	filename := f.Filename
	extension := filepath.Ext(filename)
	file, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("failed open multipart file: %w", err)
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed reade file: %w", err)
	}

	return p.storeImg(ctx, userID, isPublic, tags, data, extension)
}

func (p *PicStore) UploadImgURL(ctx context.Context, userID uint, url string, isPublic bool, tags string) (*PicImage, error) {
	data, err := downloadImage(url)
	if err != nil {
		return nil, fmt.Errorf("failed download image: %w", err)
	}

	extension := filepath.Ext(url)
	return p.storeImg(ctx, userID, isPublic, tags, data, extension)
}

func tagsFromImage(img *models.Image) string {
	if len(img.TagsRel) == 0 {
		return img.Tags
	}
	var tags []string
	for _, t := range img.TagsRel {
		tags = append(tags, t.Name)
	}
	return strings.Join(tags, " ")
}

// containsAllTags проверяет, содержит ли строка тегов изображения все запрошенные теги.
// imageTags - строка тегов, разделенных пробелами.
// searchTags - слайс тегов для поиска.
func containsAllTags(imageTags string, searchTags []string) bool {
	if len(searchTags) == 0 {
		return true
	}
	tagMap := make(map[string]bool)
	for _, t := range strings.Fields(imageTags) {
		tagMap[t] = true
	}
	for _, st := range searchTags {
		if !tagMap[st] {
			return false
		}
	}
	return true
}

func (p *PicStore) storeImg(ctx context.Context, userID uint, isPublic bool, tags string, data []byte, extension string) (*PicImage, error) {
	cur := time.Now()
	// относительный путь для БД
	storeDir := path.Join(cur.Format("2006"), cur.Format("01"), cur.Format("02"), cur.Format("15"))
	// полный путь до папки хранения
	tmpDir := path.Join(p.picturePath, storeDir)
	if _, err := os.Stat(tmpDir); err != nil && errors.Is(err, os.ErrNotExist) {
		err = os.MkdirAll(tmpDir, tools.Mode0600)
		if err != nil {
			return nil, fmt.Errorf("failed create user dir `%s`: %w", tmpDir, err)
		}
	}

	newFilename := tools.RandomString(lengthFilename) + extension
	tmpFullFilename := path.Join(tmpDir, newFilename)
	storeFullFilename := path.Join(storeDir, newFilename)
	file, err := os.Create(tmpFullFilename)
	if err != nil {
		return nil, fmt.Errorf("faild create file `%s`: %w", tmpFullFilename, err)
	}
	defer file.Close()

	_, err = file.Write(data)
	if err != nil {
		return nil, fmt.Errorf("failed save file: %w", err)
	}

	img, err := p.store.NewImage(ctx, userID, storeFullFilename, isPublic, tags)
	if err != nil {
		go func() {
			err := os.Remove(tmpFullFilename)
			if err != nil {
				p.log.Error("filed remove file", zap.String("filename", tmpFullFilename), zap.Error(err))
			}
		}()
		return nil, fmt.Errorf("failed save file to store: %w", err)
	}

	return &PicImage{
		ID:        img.ID,
		Filename:  newFilename,
		Path:      storeFullFilename,
		Extension: extensify(extension),
		IsPublic:  img.IsPublic,
		UserID:    img.UserID,
		Tags:      tagsFromImage(img),
	}, nil
}

func (p *PicStore) GetImg(ctx context.Context, path string) (*PicImage, error) {
	fullPath := filepath.Join(p.picturePath, path)

	img, err := p.store.GetImage(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed getting from store: %w", err)
	}

	return &PicImage{
		ID:        img.ID,
		IsPublic:  img.IsPublic,
		Path:      fullPath,
		Extension: extensify(filepath.Ext(path)),
		UserID:    img.UserID,
		Tags:      tagsFromImage(img),
	}, nil
}

func (p *PicStore) getImages(ctx context.Context, filter func(*PicImage) bool) ([]*PicImage, error) {
	var err error
	data := models.Images{}
	p.locker[nsImagesAll].Lock()
	if err = p.cache.GetH(ctx, nsImagesAll, &data); err != nil {
		data, err = p.store.GetImages(ctx)
		if err != nil {
			return []*PicImage{}, fmt.Errorf("failed gettings images: %w", err)
		}
		if err := p.cache.SetH(ctx, nsImagesAll, &data, time.Minute*5); err != nil {
			p.log.Error("failed cacheing images", zap.Error(err))
		}
	}
	p.locker[nsImagesAll].Unlock()

	filtered := make([]*PicImage, 0)
	for _, line := range data {
		image := &PicImage{
			ID:        line.ID,
			IsPublic:  line.IsPublic,
			Path:      line.Path,
			Extension: extensify(filepath.Ext(line.Path)),
			UserID:    line.UserID,
			Tags:      tagsFromImage(line),
		}
		if filter(image) {
			filtered = append(filtered, image)
		}
	}

	// Установка связей Prev и Next
	for i := range filtered {
		if i > 0 {
			filtered[i].Prev = filtered[i-1]
		}
		if i < len(filtered)-1 {
			filtered[i].Next = filtered[i+1]
		}
	}

	return filtered, nil
}

func (p *PicStore) GetPosts(ctx context.Context) ([]*PicImage, error) {
	var err error
	data := picImages{}

	p.locker[nsPostsPublic].Lock()
	if err = p.cache.GetH(ctx, nsPostsPublic, &data); err != nil {
		data, err = p.getImages(ctx, func(pi *PicImage) bool { return pi.IsPublic == true })
		if err != nil {
			return nil, fmt.Errorf("failed getting public posts: %w", err)
		}
		if err := p.cache.SetH(ctx, nsPostsPublic, &data, time.Minute*5); err != nil {
			p.log.Error("failed cacheing posts", zap.Error(err))
		}
	}
	p.locker[nsPostsPublic].Unlock()

	return data, nil
}

// GetPostsWithTags возвращает публичные посты, отфильтрованные по тегам.
// tags - строка тегов, разделенных пробелами.
func (p *PicStore) GetPostsWithTags(ctx context.Context, tags string) ([]*PicImage, error) {
	searchTags := strings.Fields(tags)
	// Получаем все публичные посты (используем кэш)
	posts, err := p.GetPosts(ctx)
	if err != nil {
		return nil, err
	}
	// Фильтруем по тегам
	filtered := make([]*PicImage, 0)
	for _, img := range posts {
		if containsAllTags(img.Tags, searchTags) {
			filtered = append(filtered, img)
		}
	}
	// Нужно переустановить связи Prev/Next для отфильтрованного списка
	for i := range filtered {
		if i > 0 {
			filtered[i].Prev = filtered[i-1]
		} else {
			filtered[i].Prev = nil
		}
		if i < len(filtered)-1 {
			filtered[i].Next = filtered[i+1]
		} else {
			filtered[i].Next = nil
		}
	}
	return filtered, nil
}

// GetTagsWithCount возвращает карту тегов с количеством изображений, содержащих каждый тег.
// Возвращает map[tag]count.
func (p *PicStore) GetTagsWithCount(ctx context.Context) (map[string]int, error) {
	posts, err := p.GetPosts(ctx)
	if err != nil {
		return nil, err
	}
	tagCount := make(map[string]int)
	for _, img := range posts {
		tags := strings.Fields(img.Tags)
		for _, tag := range tags {
			tagCount[tag]++
		}
	}
	return tagCount, nil
}

func (p *PicStore) GetUserPosts(ctx context.Context, userID uint) ([]*PicImage, error) {
	// Используем getImages с фильтром по userID
	return p.getImages(ctx, func(pi *PicImage) bool { return pi.UserID == userID })
}

// UpdateImage обновляет публичность и/или теги изображения, принадлежащего пользователю.
// После обновления инвалидирует кэш изображений.
func (p *PicStore) UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error {
	err := p.store.UpdateImage(ctx, userID, imageID, isPublic, tags)
	if err != nil {
		return fmt.Errorf("failed update image: %w", err)
	}
	// Инвалидируем кэш изображений
	p.locker[nsImagesAll].Lock()
	if err := p.cache.SetH(ctx, nsImagesAll, &models.Images{}, time.Millisecond); err != nil {
		p.log.Error("failed invalidate cache", zap.Error(err))
	}
	p.locker[nsImagesAll].Unlock()
	// Также инвалидируем кэш публичных постов, если изменилась публичность
	if isPublic != nil {
		p.locker[nsPostsPublic].Lock()
		if err := p.cache.SetH(ctx, nsPostsPublic, &picImages{}, time.Millisecond); err != nil {
			p.log.Error("failed invalidate posts cache", zap.Error(err))
		}
		p.locker[nsPostsPublic].Unlock()
	}
	return nil
}

// DeleteImage удаляет изображение, принадлежащее пользователю.
// После удаления инвалидирует кэш изображений.
func (p *PicStore) DeleteImage(ctx context.Context, userID uint, imageID uint) error {
	err := p.store.DelImage(ctx, userID, imageID)
	if err != nil {
		return fmt.Errorf("failed delete image: %w", err)
	}
	// Инвалидируем кэш изображений
	p.locker[nsImagesAll].Lock()
	if err := p.cache.SetH(ctx, nsImagesAll, &models.Images{}, time.Millisecond); err != nil {
		p.log.Error("failed invalidate cache", zap.Error(err))
	}
	p.locker[nsImagesAll].Unlock()
	// Инвалидируем кэш публичных постов (на всякий случай)
	p.locker[nsPostsPublic].Lock()
	if err := p.cache.SetH(ctx, nsPostsPublic, &picImages{}, time.Millisecond); err != nil {
		p.log.Error("failed invalidate posts cache", zap.Error(err))
	}
	p.locker[nsPostsPublic].Unlock()
	return nil
}
