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
}

type self interface {
	UploadImgFile(ctx context.Context, userID uint, f *multipart.FileHeader, isPublic bool, tags string) (*PicImage, error)
	UploadImgURL(ctx context.Context, userID uint, url string, isPublic bool, tags string) (*PicImage, error)
	GetImg(ctx context.Context, path string) (*PicImage, error)
	GetPosts(ctx context.Context) ([]*PicImage, error)
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

	posts := make([]*PicImage, 0)

	var prev PicImage
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
			if len(posts)-1 > 0 {
				prev = *posts[len(posts)-2]
				prev.Next = nil
				prev.Prev = nil
				image.Prev = &prev
			}
			if len(posts)-1 >= 0 {
				next := *image
				next.Prev = nil
				next.Next = nil
				posts[len(posts)-1].Next = &next
			}
			posts = append(posts, image)
		}
	}

	return posts, nil
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
