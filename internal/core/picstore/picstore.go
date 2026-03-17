package picstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
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

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"
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
	Remove(ctx context.Context, key string) error
}

type store interface {
	NewImage(ctx context.Context, userID uint, path string, isPublic bool, tags string, isEncrypted bool, salt, nonce []byte) (*models.Image, error)
	NewImageWithStatus(ctx context.Context, userID uint, path, previewPath string, isPublic bool, tags string,
		isEncrypted bool, salt, nonce, previewSalt, previewNonce []byte,
		processingStatus, originalPath, tempStoragePath string) (*models.Image, error)
	UpdateProcessingStatus(ctx context.Context, imageID uint, status string, errorMsg *string) error
	UpdateImageAfterProcessing(ctx context.Context, imageID uint, finalPath, previewPath string,
		isEncrypted bool, salt, nonce, previewSalt, previewNonce []byte,
		status string, processedAt *time.Time) error
	GetImageByID(ctx context.Context, imageID uint) (*models.Image, error)
	GetImage(ctx context.Context, path string) (*models.Image, error)
	GetImages(ctx context.Context) ([]*models.Image, error)
	DelImage(ctx context.Context, userID uint, imageID uint) error
	DelImages(ctx context.Context, userID uint, imageIDs []uint) error
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
	DeleteImages(ctx context.Context, userID uint, imageIDs []uint) error
	GetMaxFileSize() int64
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
		taskQueue: make(chan func() error, cfg.TaskQueueSize),
		ctx:       ctx,
		cancel:    cancel,
	}
	// Запускаем воркеры
	for i := 0; i < cfg.WorkerPoolSize; i++ {
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

// processImageTask выполняет асинхронную обработку изображения: конвертацию, генерацию превью, шифрование.
func (p *PicStore) processImageTask(ctx context.Context, imageID uint, encryptionKey string) error {
	start := time.Now()
	p.log.Debug("processImageTask started", zap.Uint("imageID", imageID))
	// Получаем запись изображения из БД
	img, err := p.store.GetImageByID(ctx, imageID)
	if err != nil {
		return fmt.Errorf("failed to get image by ID %d: %w", imageID, err)
	}

	// Проверяем, что изображение находится в состоянии pending или processing
	if img.ProcessingStatus != "pending" && img.ProcessingStatus != "processing" {
		p.log.Warn("image already processed or failed", zap.Uint("imageID", imageID), zap.String("status", img.ProcessingStatus))
		return nil
	}

	// Обновляем статус на processing
	if err := p.store.UpdateProcessingStatus(ctx, imageID, "processing", nil); err != nil {
		return fmt.Errorf("failed to update status to processing: %w", err)
	}

	// Определяем путь к исходному файлу (временное хранилище или основной путь)
	sourcePath := img.TempStoragePath
	if sourcePath == "" {
		sourcePath = filepath.Join(p.picturePath, img.Path)
	}

	// Читаем исходный файл
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		errorMsg := fmt.Sprintf("failed to read source file: %v", err)
		_ = p.store.UpdateProcessingStatus(ctx, imageID, "failed", &errorMsg)
		return fmt.Errorf("failed to read source file: %w", err)
	}
	p.log.Debug("source file read", zap.Uint("imageID", imageID), zap.Int("size", len(data)))

	// Если изображение зашифровано, расшифровываем
	var plainData []byte
	if img.IsEncrypted {
		// Получаем ключ шифрования из кэша (если не передан)
		key := encryptionKey
		if key == "" {
			// Попытка получить ключ из кэша
			cacheKey := fmt.Sprintf("encryption_key:%d", imageID)
			keyBytes, err := p.cache.Get(ctx, cacheKey)
			if err != nil {
				errorMsg := fmt.Sprintf("encryption key not found in cache: %v", err)
				_ = p.store.UpdateProcessingStatus(ctx, imageID, "needs_encryption_key", &errorMsg)
				return fmt.Errorf("encryption key not found: %w", err)
			}
			key = string(keyBytes)
		}
		plainData, err = p.decryptFile(data, img.Nonce, img.Salt, key)
		if err != nil {
			errorMsg := fmt.Sprintf("decryption failed: %v", err)
			_ = p.store.UpdateProcessingStatus(ctx, imageID, "failed", &errorMsg)
			return fmt.Errorf("decryption failed: %w", err)
		}
	} else {
		plainData = data
	}

	// Конвертация в целевой формат (из конфигурации)
	var convertedData []byte
	if p.cfg.ConvertToFormat != "" {
		convertedData, err = p.convertToWebP(plainData)
		if err != nil {
			errorMsg := fmt.Sprintf("conversion failed: %v", err)
			_ = p.store.UpdateProcessingStatus(ctx, imageID, "failed", &errorMsg)
			return fmt.Errorf("conversion failed: %w", err)
		}
		p.log.Debug("image converted", zap.Uint("imageID", imageID), zap.Int("converted_size", len(convertedData)))
	} else {
		convertedData = plainData
		p.log.Debug("conversion skipped", zap.Uint("imageID", imageID))
	}

	// Генерация превью (максимальная ширина из конфигурации)
	var previewData []byte
	if p.cfg.GeneratePreview {
		previewData, err = p.generatePreview(plainData, p.cfg.PreviewMaxSize)
		if err != nil {
			errorMsg := fmt.Sprintf("preview generation failed: %v", err)
			_ = p.store.UpdateProcessingStatus(ctx, imageID, "failed", &errorMsg)
			return fmt.Errorf("preview generation failed: %w", err)
		}
		p.log.Debug("preview generated", zap.Uint("imageID", imageID), zap.Int("preview_size", len(previewData)))
	} else {
		previewData = nil
		p.log.Debug("preview generation skipped", zap.Uint("imageID", imageID))
	}

	// Шифрование, если требуется
	var finalData, finalPreviewData []byte
	var finalSalt, finalNonce, previewSalt, previewNonce []byte
	isEncrypted := img.IsEncrypted
	if isEncrypted {
		// Используем тот же ключ шифрования
		key := encryptionKey
		if key == "" {
			// Ключ уже должен быть в кэше
			cacheKey := fmt.Sprintf("encryption_key:%d", imageID)
			keyBytes, _ := p.cache.Get(ctx, cacheKey)
			key = string(keyBytes)
		}
		// Шифруем основное изображение
		encryptedData, nonce, salt, err := p.encryptFile(convertedData, key)
		if err != nil {
			errorMsg := fmt.Sprintf("encryption of main image failed: %v", err)
			_ = p.store.UpdateProcessingStatus(ctx, imageID, "failed", &errorMsg)
			return fmt.Errorf("encryption of main image failed: %w", err)
		}
		finalData = encryptedData
		finalNonce = nonce
		finalSalt = salt

		// Шифруем превью, только если оно есть
		if previewData != nil {
			encryptedPreview, noncePrev, saltPrev, err := p.encryptFile(previewData, key)
			if err != nil {
				errorMsg := fmt.Sprintf("encryption of preview failed: %v", err)
				_ = p.store.UpdateProcessingStatus(ctx, imageID, "failed", &errorMsg)
				return fmt.Errorf("encryption of preview failed: %w", err)
			}
			finalPreviewData = encryptedPreview
			previewNonce = noncePrev
			previewSalt = saltPrev
		} else {
			finalPreviewData = nil
			previewNonce = nil
			previewSalt = nil
		}
	} else {
		finalData = convertedData
		finalPreviewData = previewData
	}

	// Сохраняем файлы в постоянное хранилище
	cur := time.Now()
	storeDir := path.Join(cur.Format("2006"), cur.Format("01"), cur.Format("02"), cur.Format("15"))
	fullDir := filepath.Join(p.picturePath, storeDir)
	if err := os.MkdirAll(fullDir, 0755); err != nil {
		errorMsg := fmt.Sprintf("failed to create directory: %v", err)
		_ = p.store.UpdateProcessingStatus(ctx, imageID, "failed", &errorMsg)
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Определяем расширения файлов
	mainExt := ".webp"
	if p.cfg.ConvertToFormat != "" {
		mainExt = "." + p.cfg.ConvertToFormat
	}
	previewExt := ".webp"
	if p.cfg.PreviewFormat != "" {
		previewExt = "." + p.cfg.PreviewFormat
	}

	// Генерируем имена файлов
	mainFilename := tools.RandomString(lengthFilename) + mainExt
	mainPath := filepath.Join(fullDir, mainFilename)
	var previewFilename, previewPath, relativePreviewPath string
	if previewData != nil {
		previewFilename = "preview_" + tools.RandomString(lengthFilename) + previewExt
		previewPath = filepath.Join(fullDir, previewFilename)
		relativePreviewPath = path.Join(storeDir, previewFilename)
	} else {
		previewFilename = ""
		previewPath = ""
		relativePreviewPath = ""
	}

	// Записываем основной файл
	if err := os.WriteFile(mainPath, finalData, 0644); err != nil {
		errorMsg := fmt.Sprintf("failed to write main file: %v", err)
		_ = p.store.UpdateProcessingStatus(ctx, imageID, "failed", &errorMsg)
		return fmt.Errorf("failed to write main file: %w", err)
	}
	p.log.Debug("main file written", zap.Uint("imageID", imageID), zap.String("path", mainPath))
	// Записываем превью, только если есть данные
	if finalPreviewData != nil {
		if err := os.WriteFile(previewPath, finalPreviewData, 0644); err != nil {
			errorMsg := fmt.Sprintf("failed to write preview file: %v", err)
			_ = p.store.UpdateProcessingStatus(ctx, imageID, "failed", &errorMsg)
			return fmt.Errorf("failed to write preview file: %w", err)
		}
		p.log.Debug("preview file written", zap.Uint("imageID", imageID), zap.String("path", previewPath))
	}

	// Относительные пути для БД
	relativeMainPath := path.Join(storeDir, mainFilename)

	// Обновляем запись в БД
	processedAt := time.Now()
	err = p.store.UpdateImageAfterProcessing(ctx, imageID, relativeMainPath, relativePreviewPath,
		isEncrypted, finalSalt, finalNonce, previewSalt, previewNonce,
		"completed", &processedAt)
	if err != nil {
		errorMsg := fmt.Sprintf("failed to update image after processing: %v", err)
		_ = p.store.UpdateProcessingStatus(ctx, imageID, "failed", &errorMsg)
		return fmt.Errorf("failed to update image after processing: %w", err)
	}
	p.log.Debug("database updated after processing", zap.Uint("imageID", imageID), zap.String("mainPath", relativeMainPath), zap.String("previewPath", relativePreviewPath))

	// Удаляем временный файл, если он существует
	if img.TempStoragePath != "" {
		if err := os.Remove(img.TempStoragePath); err != nil && !os.IsNotExist(err) {
			p.log.Warn("failed to remove temp file", zap.String("path", img.TempStoragePath), zap.Error(err))
		}
	}

	// Инвалидируем кэш
	p.invalidateCache(ctx)

	p.log.Info("image processing completed", zap.Uint("imageID", imageID), zap.Duration("elapsed", time.Since(start)))
	return nil
}

// Stop останавливает воркеры и освобождает ресурсы.
func (p *PicStore) Stop() {
	p.cancel()
	p.wg.Wait()
	close(p.taskQueue)
}

func (p *PicStore) UploadImgFile(ctx context.Context, userID uint, f *multipart.FileHeader, isPublic bool, tags string, encryptionKey string) (*PicImage, error) {
	// Проверка размера файла
	if p.cfg.MaxFileSize > 0 && f.Size > p.cfg.MaxFileSize {
		return nil, fmt.Errorf("размер файла превышает максимально допустимый (%d байт)", p.cfg.MaxFileSize)
	}
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
// searchTags - слайс тегов для поиска (может содержать теги с префиксом '-' для исключения).
// Сравнение регистронезависимое (теги приводятся к нижнему регистру).
func containsAllTags(imageTags string, searchTags []string) bool {
	if len(searchTags) == 0 {
		return true
	}
	// Разделяем теги на включающие (required) и исключающие (excluded)
	var required []string
	var excluded []string
	for _, st := range searchTags {
		if strings.HasPrefix(st, "-") && len(st) > 1 {
			excluded = append(excluded, strings.ToLower(st[1:]))
		} else {
			required = append(required, strings.ToLower(st))
		}
	}

	// Строим карту тегов изображения
	tagMap := make(map[string]bool)
	for _, t := range strings.Fields(imageTags) {
		tagMap[strings.ToLower(t)] = true
	}

	// Проверяем, что все required теги присутствуют
	for _, req := range required {
		if !tagMap[req] {
			return false
		}
	}
	// Проверяем, что ни один excluded тег не присутствует
	for _, exc := range excluded {
		if tagMap[exc] {
			return false
		}
	}
	return true
}

func (p *PicStore) storeImg(ctx context.Context, userID uint, isPublic bool, tags string, encryptionKey string, data []byte, extension string) (*PicImage, error) {
	// Проверка размера данных
	if p.cfg.MaxFileSize > 0 && int64(len(data)) > p.cfg.MaxFileSize {
		return nil, fmt.Errorf("размер файла превышает максимально допустимый (%d байт)", p.cfg.MaxFileSize)
	}

	cur := time.Now()
	// относительный путь для БД (используется для постоянного хранения после обработки)
	storeDir := path.Join(cur.Format("2006"), cur.Format("01"), cur.Format("02"), cur.Format("15"))
	// полный путь до папки хранения (временной или постоянной)
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

	// Сохраняем файл (временный или постоянный)
	file, err := os.Create(tmpFullFilename)
	if err != nil {
		return nil, fmt.Errorf("faild create file `%s`: %w", tmpFullFilename, err)
	}
	defer file.Close()

	_, err = file.Write(finalData)
	if err != nil {
		return nil, fmt.Errorf("failed save file: %w", err)
	}

	// Если асинхронная обработка включена, создаём запись со статусом pending и временным путём
	var img *models.Image
	if p.cfg.AsyncProcessing {
		// Временный путь для исходного файла (будет удалён после обработки)
		tempStoragePath := tmpFullFilename
		// Пока превью нет, путь пустой
		previewPath := ""
		// Оригинальный путь (относительный) - пока пустой, после обработки заполнится
		originalPath := storeFullFilename
		// Создаём запись с помощью NewImageWithStatus
		img, err = p.store.NewImageWithStatus(ctx, userID, originalPath, previewPath, isPublic, tags,
			isEncrypted, salt, nonce, nil, nil,
			"pending", originalPath, tempStoragePath)
		if err != nil {
			go func() {
				err := os.Remove(tmpFullFilename)
				if err != nil {
					p.log.Error("filed remove file", zap.String("filename", tmpFullFilename), zap.Error(err))
				}
			}()
			return nil, fmt.Errorf("failed save file to store: %w", err)
		}

		// Сохраняем ключ шифрования в кэш, если требуется
		if isEncrypted && encryptionKey != "" {
			cacheKey := fmt.Sprintf("encryption_key:%d", img.ID)
			if err := p.cache.Set(ctx, cacheKey, []byte(encryptionKey), p.cfg.KeyStorageTTL); err != nil {
				p.log.Warn("failed to cache encryption key", zap.Uint("imageID", img.ID), zap.Error(err))
				// Не прерываем загрузку, но обработка может позже завершиться ошибкой
			}
		}

		// Ставим задачу на обработку в очередь
		task := func() error {
			ctx := context.Background() // не наследуем контекст, иначе задача прервется с завершением запроса
			return p.processImageTask(ctx, img.ID, encryptionKey)
		}
		select {
		case p.taskQueue <- task:
			p.log.Debug("image processing queued", zap.Uint("imageID", img.ID))
		default:
			p.log.Warn("task queue full, image processing not queued", zap.Uint("imageID", img.ID))
			// Если очередь переполнена, можно либо подождать, либо обработать синхронно.
			// Пока просто оставляем статус pending, воркеры позже обработают.
		}
	} else {
		// Синхронный путь: создаём обычную запись без статуса
		img, err = p.store.NewImage(ctx, userID, storeFullFilename, isPublic, tags, isEncrypted, salt, nonce)
		if err != nil {
			go func() {
				err := os.Remove(tmpFullFilename)
				if err != nil {
					p.log.Error("filed remove file", zap.String("filename", tmpFullFilename), zap.Error(err))
				}
			}()
			return nil, fmt.Errorf("failed save file to store: %w", err)
		}
	}

	// Инвалидируем кэш, так как добавили новое изображение
	p.invalidateCache(ctx)

	return &PicImage{
		ID:          img.ID,
		Filename:    newFilename,
		Path:        storeFullFilename,
		PreviewPath: "", // пока превью нет, после обработки заполнится
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
	img, err := p.store.GetImage(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed getting from store: %w", err)
	}

	// Если изображение ещё обрабатывается, всё равно возвращаем метаданные, но файл может быть временным.
	// Логируем статус для отладки.
	if img.ProcessingStatus != "" && img.ProcessingStatus != "completed" {
		p.log.Debug("image not yet completed", zap.String("status", img.ProcessingStatus), zap.Uint("imageID", img.ID))
	}

	// Определяем путь к файлу: если есть временный путь, используем его, иначе обычный.
	filePath := img.Path
	if img.TempStoragePath != "" {
		filePath = img.TempStoragePath
	}
	fullPath := filepath.Join(p.picturePath, filePath)

	return &PicImage{
		ID:          img.ID,
		IsPublic:    img.IsPublic,
		Path:        fullPath,
		PreviewPath: img.PreviewPath,
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
		// Пропускаем только неудачные обработки
		if line.ProcessingStatus == "failed" {
			continue
		}
		// Пропускаем зашифрованные изображения, если требуется
		if skipEncrypted && line.IsEncrypted {
			continue
		}
		image := &PicImage{
			ID:          line.ID,
			IsPublic:    line.IsPublic,
			Path:        line.Path,
			PreviewPath: line.PreviewPath,
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
			func(img PicImage) {
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

				// Отправляем задачу на удаление файла в очередь
				fullPreviewPath := filepath.Join(p.picturePath, img.PreviewPath)
				task = func() error {
					if err := os.Remove(fullPreviewPath); err != nil && !os.IsNotExist(err) {
						return fmt.Errorf("failed to delete image file %s: %w", fullPreviewPath, err)
					}
					return nil
				}
				select {
				case p.taskQueue <- task:
					p.log.Debug("file deletion queued", zap.String("path", fullPreviewPath))
				default:
					p.log.Warn("task queue full, file not queued", zap.String("path", fullPreviewPath))

				}
			}(*img)
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

// GetMaxFileSize возвращает максимально допустимый размер файла в байтах.
func (p *PicStore) GetMaxFileSize() int64 {
	return p.cfg.MaxFileSize
}

// convertToWebP конвертирует изображение в целевой формат (из конфигурации).
// Поддерживаемые форматы: webp, jpeg, png.
func (p *PicStore) convertToWebP(data []byte) ([]byte, error) {
	start := time.Now()
	p.log.Debug("convertToWebP started", zap.Int("input_size", len(data)), zap.String("target_format", p.cfg.ConvertToFormat))

	// Декодируем изображение
	img, err := imaging.Decode(bytes.NewReader(data))
	if err != nil {
		p.log.Error("failed to decode image", zap.Error(err))
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	var output []byte
	switch strings.ToLower(p.cfg.ConvertToFormat) {
	case "webp":
		// Кодируем в WebP с указанным качеством
		quality := float32(p.cfg.Quality)
		if quality < 0 {
			quality = 85
		}
		if quality > 100 {
			quality = 100
		}
		output, err = webp.EncodeRGBA(img, quality)
		if err != nil {
			p.log.Error("failed to encode webp", zap.Error(err))
			return nil, fmt.Errorf("failed to encode webp: %w", err)
		}
	case "jpeg", "jpg":
		// Кодируем в JPEG с указанным качеством
		quality := p.cfg.Quality
		if quality < 1 {
			quality = 85
		}
		if quality > 100 {
			quality = 100
		}
		buf := new(bytes.Buffer)
		err = imaging.Encode(buf, img, imaging.JPEG, imaging.JPEGQuality(quality))
		if err != nil {
			p.log.Error("failed to encode jpeg", zap.Error(err))
			return nil, fmt.Errorf("failed to encode jpeg: %w", err)
		}
		output = buf.Bytes()
	case "png":
		// Кодируем в PNG с дефолтным сжатием
		buf := new(bytes.Buffer)
		err = imaging.Encode(buf, img, imaging.PNG)
		if err != nil {
			p.log.Error("failed to encode png", zap.Error(err))
			return nil, fmt.Errorf("failed to encode png: %w", err)
		}
		output = buf.Bytes()
	default:
		// Если формат не поддерживается, возвращаем исходные данные
		p.log.Warn("unsupported target format, returning original", zap.String("format", p.cfg.ConvertToFormat))
		output = data
	}

	elapsed := time.Since(start)
	p.log.Debug("convertToWebP completed",
		zap.Int("output_size", len(output)),
		zap.Duration("elapsed", elapsed))
	return output, nil
}

// generatePreview создаёт превью изображения с максимальной шириной maxWidth.
// Формат превью берётся из конфигурации (PreviewFormat), качество из PreviewQuality.
func (p *PicStore) generatePreview(data []byte, maxWidth int) ([]byte, error) {
	start := time.Now()
	p.log.Debug("generatePreview started",
		zap.Int("input_size", len(data)),
		zap.Int("maxWidth", maxWidth),
		zap.String("preview_format", p.cfg.PreviewFormat),
		zap.Int("preview_quality", p.cfg.PreviewQuality))

	// Декодируем изображение
	img, err := imaging.Decode(bytes.NewReader(data))
	if err != nil {
		p.log.Error("failed to decode image for preview", zap.Error(err))
		return nil, fmt.Errorf("failed to decode image for preview: %w", err)
	}

	// Получаем размеры исходного изображения
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Если изображение уже меньше maxWidth, не масштабируем
	var resized image.Image
	if width > maxWidth || height > maxWidth {
		// Масштабируем, сохраняя пропорции, чтобы большая сторона была maxWidth
		resized = imaging.Resize(img, maxWidth, 0, imaging.Lanczos)
	} else {
		resized = img
	}

	// Кодируем в целевой формат превью
	quality := p.cfg.PreviewQuality
	if quality < 1 {
		quality = 75
	}
	if quality > 100 {
		quality = 100
	}
	var output []byte
	switch strings.ToLower(p.cfg.PreviewFormat) {
	case "webp":
		output, err = webp.EncodeRGBA(resized, float32(quality))
		if err != nil {
			p.log.Error("failed to encode preview webp", zap.Error(err))
			return nil, fmt.Errorf("failed to encode preview webp: %w", err)
		}
	case "jpeg", "jpg":
		buf := new(bytes.Buffer)
		err = imaging.Encode(buf, resized, imaging.JPEG, imaging.JPEGQuality(quality))
		if err != nil {
			p.log.Error("failed to encode preview jpeg", zap.Error(err))
			return nil, fmt.Errorf("failed to encode preview jpeg: %w", err)
		}
		output = buf.Bytes()
	case "png":
		compressionLevel := png.NoCompression
		if quality >= 75 && quality <= 100 {
			compressionLevel = png.DefaultCompression
		}
		if quality < 75 && quality > 50 {
			compressionLevel = png.BestSpeed
		}
		if quality < 50 && quality > 25 {
			compressionLevel = png.BestCompression
		}
		buf := new(bytes.Buffer)
		err = imaging.Encode(buf, resized, imaging.PNG, imaging.PNGCompressionLevel(compressionLevel))
		if err != nil {
			p.log.Error("failed to encode preview png", zap.Error(err))
			return nil, fmt.Errorf("failed to encode preview png: %w", err)
		}
		output = buf.Bytes()
	default:
		// Если формат не поддерживается, используем WebP по умолчанию
		p.log.Warn("unsupported preview format, using webp", zap.String("format", p.cfg.PreviewFormat))
		output, err = webp.EncodeRGBA(resized, float32(quality))
		if err != nil {
			return nil, fmt.Errorf("failed to encode default webp preview: %w", err)
		}
	}

	elapsed := time.Since(start)
	p.log.Debug("generatePreview completed",
		zap.Int("output_size", len(output)),
		zap.Duration("elapsed", elapsed))
	return output, nil
}
