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
	workerCount   int    = 3
)

type cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	GetH(ctx context.Context, key string, obj types.ObjInterface) (err error)
	SetH(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error
	Remove(ctx context.Context, key string) error
}

type store interface {
	NewImage(ctx context.Context, userID uint, path string, isPublic bool, tags string, isEncrypted bool, salt, nonce []byte) (*models.Image, error)
	DelImage(ctx context.Context, userID uint, imageID uint) error
	DelImages(ctx context.Context, userID uint, imageIDs []uint) error
	GetImage(ctx context.Context, path string) (*models.Image, error)
	GetImages(ctx context.Context) ([]*models.Image, error)
	UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error
}

type self interface {
	UploadImgFile(ctx context.Context, userID uint, f *multipart.FileHeader, isPublic bool, tags string, encryptionKey string) (*PicImage, error)
	UploadImgURL(ctx context.Context, userID uint, url string, isPublic bool, tags string, encryptionKey string) (*PicImage, error)
	UploadMultipleImgFiles(ctx context.Context, userID uint, files []*multipart.FileHeader, isPublic bool, tags string, encryptionKey string) ([]*PicImage, error)
	UploadMultipleImgURLs(ctx context.Context, userID uint, urls []string, isPublic bool, tags string, encryptionKey string) ([]*PicImage, error)
	GetImg(ctx context.Context, path string) (*PicImage, error)
	DecryptImage(ctx context.Context, path string, key string) ([]byte, error)
	GetPosts(ctx context.Context) ([]*PicImage, error)
	GetPostsWithTags(ctx context.Context, tags string) ([]*PicImage, error)
	GetPostsPage(ctx context.Context, page, pageSize int, tags string) ([]*PicImage, error)
	GetTagsWithCount(ctx context.Context) (map[string]int, error)
	GetUserPosts(ctx context.Context, userID uint) ([]*PicImage, error)
	UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error
	DeleteImage(ctx context.Context, userID uint, imageID uint) error
	DeleteImages(ctx context.Context, userID uint, imageIDs []uint) error
}

type PicStore struct {
	store       store
	log         *logger.Logger
	cache       cache
	cfg         Config
	picturePath string
	locker      map[string]*sync.Mutex
	taskQueue   chan func() error
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
}

var (
	_ self = &PicStore{}
)

func New(ctx context.Context, cfg Config, log *logger.Logger, store store, cache cache) (*PicStore, error) {
	ctx, cancel := context.WithCancel(ctx)
	p := &PicStore{
		store:       store,
		log:         log,
		cache:       cache,
		cfg:         cfg,
		picturePath: cfg.PicPath,
		locker: map[string]*sync.Mutex{
			nsImagesAll:   &sync.Mutex{},
			nsPostsPublic: &sync.Mutex{},
		},
		taskQueue: make(chan func() error, 1000),
		ctx:       ctx,
		cancel:    cancel,
	}
	// Запускаем воркеры
	for i := 0; i < workerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	return p, nil
}

func (p *PicStore) worker(id int) {
	defer p.wg.Done()
	for {
		select {
		case task := <-p.taskQueue:
			if err := task(); err != nil {
				p.log.Warn("worker task failed",
					zap.Int("worker", id),
					zap.Error(err))
			} else {
				p.log.Debug("worker task completed",
					zap.Int("worker", id))
			}
		case <-p.ctx.Done():
			p.log.Debug("worker stopping", zap.Int("worker", id))
			return
		}
	}
}

// Stop останавливает воркеры и освобождает ресурсы.
func (p *PicStore) Stop() {
	p.cancel()
	p.wg.Wait()
	close(p.taskQueue)
}

func (p *PicStore) UploadImgFile(ctx context.Context, userID uint, f *multipart.FileHeader, isPublic bool, tags string, encryptionKey string) (*PicImage, error) {
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

	return p.storeImg(ctx, userID, isPublic, tags, encryptionKey, data, extension)
}

func (p *PicStore) UploadImgURL(ctx context.Context, userID uint, url string, isPublic bool, tags string, encryptionKey string) (*PicImage, error) {
	data, err := downloadImage(url)
	if err != nil {
		return nil, fmt.Errorf("failed download image: %w", err)
	}

	extension := filepath.Ext(url)
	return p.storeImg(ctx, userID, isPublic, tags, encryptionKey, data, extension)
}

func (p *PicStore) UploadMultipleImgFiles(ctx context.Context, userID uint, files []*multipart.FileHeader, isPublic bool, tags string, encryptionKey string) ([]*PicImage, error) {
	results := make([]*PicImage, 0, len(files))
	var errs []error
	for _, f := range files {
		img, err := p.UploadImgFile(ctx, userID, f, isPublic, tags, encryptionKey)
		if err != nil {
			errs = append(errs, fmt.Errorf("file %s: %w", f.Filename, err))
			continue
		}
		results = append(results, img)
	}
	if len(errs) > 0 {
		// Возвращаем результаты и объединённую ошибку
		return results, fmt.Errorf("некоторые файлы не загружены: %v", errs)
	}
	return results, nil
}

func (p *PicStore) UploadMultipleImgURLs(ctx context.Context, userID uint, urls []string, isPublic bool, tags string, encryptionKey string) ([]*PicImage, error) {
	results := make([]*PicImage, 0, len(urls))
	var errs []error
	for _, url := range urls {
		img, err := p.UploadImgURL(ctx, userID, url, isPublic, tags, encryptionKey)
		if err != nil {
			errs = append(errs, fmt.Errorf("URL %s: %w", url, err))
			continue
		}
		results = append(results, img)
	}
	if len(errs) > 0 {
		return results, fmt.Errorf("некоторые URL не загружены: %v", errs)
	}
	return results, nil
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
// Сравнение регистронезависимое (теги приводятся к нижнему регистру).
func containsAllTags(imageTags string, searchTags []string) bool {
	if len(searchTags) == 0 {
		return true
	}
	tagMap := make(map[string]bool)
	for _, t := range strings.Fields(imageTags) {
		tagMap[strings.ToLower(t)] = true
	}
	for _, st := range searchTags {
		if !tagMap[strings.ToLower(st)] {
			return false
		}
	}
	return true
}

func (p *PicStore) storeImg(ctx context.Context, userID uint, isPublic bool, tags string, encryptionKey string, data []byte, extension string) (*PicImage, error) {
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

	// Шифрование, если указан ключ
	var salt, nonce []byte
	var finalData = data
	isEncrypted := false
	if encryptionKey != "" && p.cfg.EnableEncryption {
		encryptedData, fileNonce, generatedSalt, err := p.encryptFile(data, encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt file: %w", err)
		}
		finalData = encryptedData
		nonce = fileNonce
		salt = generatedSalt
		isEncrypted = true
	}

	file, err := os.Create(tmpFullFilename)
	if err != nil {
		return nil, fmt.Errorf("faild create file `%s`: %w", tmpFullFilename, err)
	}
	defer file.Close()

	_, err = file.Write(finalData)
	if err != nil {
		return nil, fmt.Errorf("failed save file: %w", err)
	}

	img, err := p.store.NewImage(ctx, userID, storeFullFilename, isPublic, tags, isEncrypted, salt, nonce)
	if err != nil {
		go func() {
			err := os.Remove(tmpFullFilename)
			if err != nil {
				p.log.Error("filed remove file", zap.String("filename", tmpFullFilename), zap.Error(err))
			}
		}()
		return nil, fmt.Errorf("failed save file to store: %w", err)
	}

	// Инвалидируем кэш, так как добавили новое изображение
	p.invalidateCache(ctx)

	return &PicImage{
		ID:          img.ID,
		Filename:    newFilename,
		Path:        storeFullFilename,
		Extension:   extensify(extension),
		IsPublic:    img.IsPublic,
		UserID:      img.UserID,
		Tags:        tagsFromImage(img),
		IsEncrypted: img.IsEncrypted,
		Salt:        img.Salt,
		Nonce:       img.Nonce,
	}, nil
}

func (p *PicStore) GetImg(ctx context.Context, path string) (*PicImage, error) {
	fullPath := filepath.Join(p.picturePath, path)

	img, err := p.store.GetImage(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed getting from store: %w", err)
	}

	return &PicImage{
		ID:          img.ID,
		IsPublic:    img.IsPublic,
		Path:        fullPath,
		Extension:   extensify(filepath.Ext(path)),
		UserID:      img.UserID,
		Tags:        tagsFromImage(img),
		IsEncrypted: img.IsEncrypted,
		Salt:        img.Salt,
		Nonce:       img.Nonce,
	}, nil
}

func (p *PicStore) getImages(ctx context.Context, filter func(*PicImage) bool, skipEncrypted bool) ([]*PicImage, error) {
	var err error
	data := models.Images{}
	p.locker[nsImagesAll].Lock()
	if err = p.cache.GetH(ctx, nsImagesAll, &data); err != nil {
		data, err = p.store.GetImages(ctx)
		if err != nil {
			return []*PicImage{}, fmt.Errorf("failed gettings images: %w", err)
		}
		if err := p.cache.SetH(ctx, nsImagesAll, &data, p.cfg.CacheTTL); err != nil {
			p.log.Error("failed cacheing images", zap.Error(err))
		}
	}
	p.locker[nsImagesAll].Unlock()

	filtered := make([]*PicImage, 0)
	for _, line := range data {
		// Пропускаем зашифрованные изображения, если требуется
		if skipEncrypted && line.IsEncrypted {
			continue
		}
		image := &PicImage{
			ID:          line.ID,
			IsPublic:    line.IsPublic,
			Path:        line.Path,
			Extension:   extensify(filepath.Ext(line.Path)),
			UserID:      line.UserID,
			Tags:        tagsFromImage(line),
			IsEncrypted: line.IsEncrypted,
			Salt:        line.Salt,
			Nonce:       line.Nonce,
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
		data, err = p.getImages(ctx, func(pi *PicImage) bool { return pi.IsPublic == true }, true)
		if err != nil {
			return nil, fmt.Errorf("failed getting public posts: %w", err)
		}
		if err := p.cache.SetH(ctx, nsPostsPublic, &data, p.cfg.CacheTTL); err != nil {
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

// GetPostsPage возвращает страницу публичных постов с возможной фильтрацией по тегам.
// page - номер страницы (начиная с 1), pageSize - размер страницы.
func (p *PicStore) GetPostsPage(ctx context.Context, page, pageSize int, tags string) ([]*PicImage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}

	// Формируем ключ кэша для страницы
	key := fmt.Sprintf("posts:page:%d:size:%d:tags:%s", page, pageSize, tags)
	var data picImages

	// Пытаемся получить из кэша
	if err := p.cache.GetH(ctx, key, &data); err == nil {
		return data, nil
	}

	// Получаем все посты (с фильтром по тегам если нужно)
	var allPosts []*PicImage
	var err error
	if tags == "" {
		allPosts, err = p.GetPosts(ctx)
	} else {
		allPosts, err = p.GetPostsWithTags(ctx, tags)
	}
	if err != nil {
		return nil, err
	}

	// Применяем пагинацию
	start := (page - 1) * pageSize
	if start >= len(allPosts) {
		return []*PicImage{}, nil
	}
	end := start + pageSize
	if end > len(allPosts) {
		end = len(allPosts)
	}
	pagePosts := allPosts[start:end]

	// Преобразуем в picImages для кэширования
	cacheData := picImages(pagePosts)
	// Кэшируем страницу с тем же TTL, что и общий кэш
	if err := p.cache.SetH(ctx, key, &cacheData, p.cfg.CacheTTL); err != nil {
		p.log.Error("failed caching page", zap.Error(err))
	}
	return pagePosts, nil
}

// invalidateCache очищает кэши images:all и posts:public
func (p *PicStore) invalidateCache(ctx context.Context) {
	p.locker[nsImagesAll].Lock()
	_ = p.cache.Remove(ctx, nsImagesAll) // удаляем кэш
	p.locker[nsImagesAll].Unlock()

	p.locker[nsPostsPublic].Lock()
	_ = p.cache.Remove(ctx, nsPostsPublic) // удаляем кэш
	p.locker[nsPostsPublic].Unlock()
}

// UpdateImage обновляет изображение (публичность, теги)
func (p *PicStore) UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error {
	err := p.store.UpdateImage(ctx, userID, imageID, isPublic, tags)
	if err != nil {
		return err
	}
	// Инвалидируем кэш, так как данные изменились
	p.invalidateCache(ctx)
	return nil
}

// DeleteImage удаляет изображение
func (p *PicStore) DeleteImage(ctx context.Context, userID uint, imageID uint) error {
	// Получаем изображение, чтобы узнать путь к файлу
	images, err := p.GetUserPosts(ctx, userID)
	if err != nil {
		return err
	}
	var target *PicImage
	for _, img := range images {
		if img.ID == imageID {
			target = img
			break
		}
	}
	if target == nil {
		return fmt.Errorf("image not found or access denied")
	}
	// Удаляем запись из хранилища
	err = p.store.DelImage(ctx, userID, imageID)
	if err != nil {
		return err
	}
	// Отправляем задачу на удаление файла в очередь
	fullPath := filepath.Join(p.picturePath, target.Path)
	task := func() error {
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete image file %s: %w", fullPath, err)
		}
		return nil
	}
	select {
	case p.taskQueue <- task:
		p.log.Debug("file deletion queued", zap.String("path", fullPath))
	default:
		p.log.Warn("task queue full, file not queued", zap.String("path", fullPath))
	}
	p.invalidateCache(ctx)
	return nil
}

// DeleteImages удаляет несколько изображений
func (p *PicStore) DeleteImages(ctx context.Context, userID uint, imageIDs []uint) error {
	// Получаем все изображения пользователя
	images, err := p.GetUserPosts(ctx, userID)
	if err != nil {
		return err
	}
	// Создаем множество ID для быстрой проверки
	idSet := make(map[uint]bool)
	for _, id := range imageIDs {
		idSet[id] = true
	}
	// Удаляем записи из хранилища
	err = p.store.DelImages(ctx, userID, imageIDs)
	if err != nil {
		return err
	}
	// Отправляем задачи на удаление файлов в очередь
	for _, img := range images {
		if idSet[img.ID] {
			fullPath := filepath.Join(p.picturePath, img.Path)
			task := func() error {
				if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to delete image file %s: %w", fullPath, err)
				}
				return nil
			}
			select {
			case p.taskQueue <- task:
				p.log.Debug("file deletion queued", zap.String("path", fullPath))
			default:
				p.log.Warn("task queue full, file not queued", zap.String("path", fullPath))
			}
		}
	}
	p.invalidateCache(ctx)
	return nil
}

// GetTagsWithCount возвращает карту тегов с количеством их использования в публичных постах
func (p *PicStore) GetTagsWithCount(ctx context.Context) (map[string]int, error) {
	posts, err := p.GetPosts(ctx)
	if err != nil {
		return nil, err
	}
	tagCount := make(map[string]int)
	for _, post := range posts {
		tags := strings.Fields(post.Tags)
		for _, tag := range tags {
			tagCount[tag]++
		}
	}
	return tagCount, nil
}

// GetUserPosts возвращает все изображения пользователя (включая приватные)
func (p *PicStore) GetUserPosts(ctx context.Context, userID uint) ([]*PicImage, error) {
	// Пока используем getImages без фильтра по публичности, но с фильтром по пользователю
	allImages, err := p.getImages(ctx, func(pi *PicImage) bool { return pi.UserID == userID }, false)
	if err != nil {
		return nil, err
	}
	return allImages, nil
}

// DecryptImage расшифровывает изображение по пути с использованием предоставленного ключа.
// Возвращает расшифрованные данные или ошибку, если ключ неверный или изображение не зашифровано.
func (p *PicStore) DecryptImage(ctx context.Context, path string, key string) ([]byte, error) {
	// Получаем метаданные изображения
	img, err := p.GetImg(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to get image: %w", err)
	}
	if !img.IsEncrypted {
		// Если изображение не зашифровано, возвращаем его данные как есть
		return img.GetData(), nil
	}
	if len(img.Nonce) == 0 {
		return nil, fmt.Errorf("image is encrypted but missing nonce")
	}
	// Читаем зашифрованные данные из файла
	encryptedData, err := os.ReadFile(img.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read encrypted file: %w", err)
	}
	// Расшифровываем
	plaintext, err := p.decryptFile(encryptedData, img.Nonce, img.Salt, key)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}
	return plaintext, nil
}
