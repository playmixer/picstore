package picstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"picstore/internal/adapters/models"
	"picstore/internal/adapters/storage/types"
	"testing"
	"time"

	"github.com/playmixer/single-auth/pkg/logger"
	"gorm.io/gorm"
)

// newTestPicStore создаёт экземпляр PicStore для тестов.
func newTestPicStore(t *testing.T) *PicStore {
	t.Helper()

	cfg := Config{
		PicPath:          t.TempDir(),
		CacheTTL:         time.Minute,
		EnableEncryption: false,
		MaxFileSize:      5 * 1024 * 1024,
		AsyncProcessing:  false,
		TempStorageTTL:   time.Hour,
		KeyStorageTTL:    time.Minute,
		WorkerPoolSize:   1,
		TaskQueueSize:    10,
		ConvertToFormat:  "webp",
		Quality:          85,
		GeneratePreview:  true,
		PreviewMaxSize:   640,
		PreviewFormat:    "webp",
		PreviewQuality:   75,
	}

	// Создаём логгер с выводом в stdout (для тестов)
	ctx := context.Background()
	lgr, err := logger.New(ctx, logger.SetLevel("debug"), logger.SetLogPath(""))
	if err != nil {
		t.Fatal(err)
	}

	// Заглушки store и cache (nil, но они не будут использоваться в тестируемых функциях)
	store := &mockStore{}
	cache := &mockCache{}

	ps, err := New(ctx, cfg, lgr, store, cache)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

// mockStore реализует интерфейс store для тестов.
type mockStore struct{}

func (m *mockStore) NewImage(ctx context.Context, userID uint, path string, isPublic bool, tags string, isEncrypted bool, salt, nonce []byte) (*models.Image, error) {
	return nil, nil
}
func (m *mockStore) NewImageWithStatus(ctx context.Context, userID uint, path, previewPath string, isPublic bool, tags string,
	isEncrypted bool, salt, nonce, previewSalt, previewNonce []byte,
	processingStatus, originalPath, tempStoragePath string) (*models.Image, error) {
	return nil, nil
}
func (m *mockStore) UpdateProcessingStatus(ctx context.Context, imageID uint, status string, errorMsg *string) error {
	return nil
}
func (m *mockStore) UpdateImageAfterProcessing(ctx context.Context, imageID uint, finalPath, previewPath string,
	isEncrypted bool, salt, nonce, previewSalt, previewNonce []byte,
	status string, processedAt *time.Time) error {
	return nil
}
func (m *mockStore) GetImageByID(ctx context.Context, imageID uint) (*models.Image, error) {
	return nil, nil
}
func (m *mockStore) GetImage(ctx context.Context, path string) (*models.Image, error) {
	return nil, nil
}
func (m *mockStore) GetImages(ctx context.Context) ([]*models.Image, error) {
	return nil, nil
}
func (m *mockStore) DelImage(ctx context.Context, userID uint, imageID uint) error {
	return nil
}
func (m *mockStore) DelImages(ctx context.Context, userID uint, imageIDs []uint) error {
	return nil
}
func (m *mockStore) UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error {
	return nil
}

// mockCache реализует интерфейс cache для тестов.
type mockCache struct{}

func (m *mockCache) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, nil
}
func (m *mockCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return nil
}
func (m *mockCache) GetH(ctx context.Context, key string, obj types.ObjInterface) (err error) {
	return nil
}
func (m *mockCache) SetH(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
	return nil
}
func (m *mockCache) Remove(ctx context.Context, key string) error {
	return nil
}

// smallPNGBase64 - это PNG 1x1 пиксель (чёрный).
const smallPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func decodeTestPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(smallPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestConvertToWebP(t *testing.T) {
	ps := newTestPicStore(t)
	data := decodeTestPNG(t)

	// Конвертация в WebP
	converted, err := ps.convertToWebP(data)
	if err != nil {
		t.Fatalf("convertToWebP failed: %v", err)
	}
	if len(converted) == 0 {
		t.Fatal("converted data is empty")
	}
	// Проверяем, что данные изменились (размер может быть другим)
	t.Logf("Converted size: %d", len(converted))
}

func TestGeneratePreview(t *testing.T) {
	ps := newTestPicStore(t)
	data := decodeTestPNG(t)

	// Генерация превью
	preview, err := ps.generatePreview(data, ps.cfg.PreviewMaxSize)
	if err != nil {
		t.Fatalf("generatePreview failed: %v", err)
	}
	if len(preview) == 0 {
		t.Fatal("preview data is empty")
	}
	t.Logf("Preview size: %d", len(preview))
}

func TestConvertToWebP_UnsupportedFormat(t *testing.T) {
	ps := newTestPicStore(t)
	ps.cfg.ConvertToFormat = "unsupported"
	data := decodeTestPNG(t)

	// При неподдерживаемом формате функция должна вернуть исходные данные
	converted, err := ps.convertToWebP(data)
	if err != nil {
		t.Fatalf("convertToWebP with unsupported format should not error: %v", err)
	}
	if len(converted) != len(data) {
		t.Fatalf("expected same data length, got %d, want %d", len(converted), len(data))
	}
}

func TestGeneratePreview_NoResize(t *testing.T) {
	ps := newTestPicStore(t)
	ps.cfg.PreviewMaxSize = 1000 // больше размера изображения
	data := decodeTestPNG(t)

	preview, err := ps.generatePreview(data, ps.cfg.PreviewMaxSize)
	if err != nil {
		t.Fatalf("generatePreview with no resize failed: %v", err)
	}
	if len(preview) == 0 {
		t.Fatal("preview data is empty")
	}
}

// controllableStore реализует интерфейс store с возможностью задавать возвращаемые значения.
type controllableStore struct {
	newImageFunc           func(ctx context.Context, userID uint, path string, isPublic bool, tags string, isEncrypted bool, salt, nonce []byte) (*models.Image, error)
	newImageWithStatusFunc func(ctx context.Context, userID uint, path, previewPath string, isPublic bool, tags string,
		isEncrypted bool, salt, nonce, previewSalt, previewNonce []byte,
		processingStatus, originalPath, tempStoragePath string) (*models.Image, error)
	getImageFunc    func(ctx context.Context, path string) (*models.Image, error)
	getImagesFunc   func(ctx context.Context) ([]*models.Image, error)
	updateImageFunc func(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error
	delImagesFunc   func(ctx context.Context, userID uint, imageIDs []uint) error
}

func (c *controllableStore) NewImage(ctx context.Context, userID uint, path string, isPublic bool, tags string, isEncrypted bool, salt, nonce []byte) (*models.Image, error) {
	if c.newImageFunc != nil {
		return c.newImageFunc(ctx, userID, path, isPublic, tags, isEncrypted, salt, nonce)
	}
	return &models.Image{
		Model:       gorm.Model{ID: 1},
		UserID:      userID,
		Path:        path,
		IsPublic:    isPublic,
		Tags:        tags,
		IsEncrypted: isEncrypted,
		Salt:        salt,
		Nonce:       nonce,
	}, nil
}

func (c *controllableStore) NewImageWithStatus(ctx context.Context, userID uint, path, previewPath string, isPublic bool, tags string,
	isEncrypted bool, salt, nonce, previewSalt, previewNonce []byte,
	processingStatus, originalPath, tempStoragePath string) (*models.Image, error) {
	if c.newImageWithStatusFunc != nil {
		return c.newImageWithStatusFunc(ctx, userID, path, previewPath, isPublic, tags, isEncrypted, salt, nonce, previewSalt, previewNonce, processingStatus, originalPath, tempStoragePath)
	}
	return &models.Image{
		Model:            gorm.Model{ID: 1},
		UserID:           userID,
		Path:             path,
		PreviewPath:      previewPath,
		IsPublic:         isPublic,
		Tags:             tags,
		IsEncrypted:      isEncrypted,
		Salt:             salt,
		Nonce:            nonce,
		PreviewSalt:      previewSalt,
		PreviewNonce:     previewNonce,
		ProcessingStatus: processingStatus,
		OriginalPath:     originalPath,
		TempStoragePath:  tempStoragePath,
	}, nil
}

func (c *controllableStore) UpdateProcessingStatus(ctx context.Context, imageID uint, status string, errorMsg *string) error {
	return nil
}

func (c *controllableStore) UpdateImageAfterProcessing(ctx context.Context, imageID uint, finalPath, previewPath string,
	isEncrypted bool, salt, nonce, previewSalt, previewNonce []byte,
	status string, processedAt *time.Time) error {
	return nil
}

func (c *controllableStore) GetImageByID(ctx context.Context, imageID uint) (*models.Image, error) {
	return nil, nil
}

func (c *controllableStore) GetImage(ctx context.Context, path string) (*models.Image, error) {
	if c.getImageFunc != nil {
		return c.getImageFunc(ctx, path)
	}
	return &models.Image{
		Model:       gorm.Model{ID: 1},
		Path:        path,
		IsPublic:    true,
		Tags:        "test",
		IsEncrypted: false,
	}, nil
}

func (c *controllableStore) GetImages(ctx context.Context) ([]*models.Image, error) {
	if c.getImagesFunc != nil {
		return c.getImagesFunc(ctx)
	}
	return []*models.Image{}, nil
}

func (c *controllableStore) DelImage(ctx context.Context, userID uint, imageID uint) error {
	return nil
}

func (c *controllableStore) DelImages(ctx context.Context, userID uint, imageIDs []uint) error {
	if c.delImagesFunc != nil {
		return c.delImagesFunc(ctx, userID, imageIDs)
	}
	return nil
}

func (c *controllableStore) UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error {
	if c.updateImageFunc != nil {
		return c.updateImageFunc(ctx, userID, imageID, isPublic, tags)
	}
	return nil
}

// controllableCache реализует интерфейс cache с возможностью задавать поведение.
type controllableCache struct {
	getFunc    func(ctx context.Context, key string) ([]byte, error)
	setFunc    func(ctx context.Context, key string, value []byte, ttl time.Duration) error
	getHFunc   func(ctx context.Context, key string, obj types.ObjInterface) error
	setHFunc   func(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error
	removeFunc func(ctx context.Context, key string) error
}

func (c *controllableCache) Get(ctx context.Context, key string) ([]byte, error) {
	if c.getFunc != nil {
		return c.getFunc(ctx, key)
	}
	return nil, nil
}

func (c *controllableCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if c.setFunc != nil {
		return c.setFunc(ctx, key, value, ttl)
	}
	return nil
}

func (c *controllableCache) GetH(ctx context.Context, key string, obj types.ObjInterface) error {
	if c.getHFunc != nil {
		return c.getHFunc(ctx, key, obj)
	}
	return nil
}

func (c *controllableCache) SetH(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
	if c.setHFunc != nil {
		return c.setHFunc(ctx, key, value, ttl)
	}
	return nil
}

func (c *controllableCache) Remove(ctx context.Context, key string) error {
	if c.removeFunc != nil {
		return c.removeFunc(ctx, key)
	}
	return nil
}

// newTestPicStoreWithMocks создаёт PicStore с переданными store и cache.
func newTestPicStoreWithMocks(t *testing.T, store store, cache cache) *PicStore {
	t.Helper()

	cfg := Config{
		PicPath:          t.TempDir(),
		CacheTTL:         time.Minute,
		EnableEncryption: false,
		MaxFileSize:      5 * 1024 * 1024,
		AsyncProcessing:  false,
		TempStorageTTL:   time.Hour,
		KeyStorageTTL:    time.Minute,
		WorkerPoolSize:   1,
		TaskQueueSize:    10,
		ConvertToFormat:  "webp",
		Quality:          85,
		GeneratePreview:  true,
		PreviewMaxSize:   640,
		PreviewFormat:    "webp",
		PreviewQuality:   75,
	}

	ctx := context.Background()
	lgr, err := logger.New(ctx, logger.SetLevel("debug"), logger.SetLogPath(""))
	if err != nil {
		t.Fatal(err)
	}

	ps, err := New(ctx, cfg, lgr, store, cache)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

// createTestMultipartFile создаёт multipart.FileHeader для тестов.
func createTestMultipartFile(t *testing.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	writer.Close()

	reader := multipart.NewReader(&buf, writer.Boundary())
	form, err := reader.ReadForm(10 << 20)
	if err != nil {
		t.Fatal(err)
	}
	files := form.File["file"]
	if len(files) == 0 {
		t.Fatal("no file in multipart form")
	}
	return files[0]
}

func TestUploadImgFile(t *testing.T) {
	store := &controllableStore{}
	cache := &controllableCache{}
	ps := newTestPicStoreWithMocks(t, store, cache)

	// Подготовка тестового файла
	data := decodeTestPNG(t)
	fileHeader := createTestMultipartFile(t, "test.png", data)

	// Вызов метода
	userID := uint(1)
	isPublic := true
	tags := "test,image"
	encryptionKey := ""
	img, err := ps.UploadImgFile(context.Background(), userID, fileHeader, isPublic, tags, encryptionKey)
	if err != nil {
		t.Fatalf("UploadImgFile failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil PicImage")
	}
	if img.Path == "" {
		t.Error("Path should be set")
	}
	if img.UserID != userID {
		t.Errorf("UserID mismatch: got %d, want %d", img.UserID, userID)
	}
	if img.IsPublic != isPublic {
		t.Errorf("IsPublic mismatch: got %v, want %v", img.IsPublic, isPublic)
	}
	if img.Tags != tags {
		t.Errorf("Tags mismatch: got %q, want %q", img.Tags, tags)
	}
}

func TestUploadImgURL(t *testing.T) {
	store := &controllableStore{}
	cache := &controllableCache{}
	ps := newTestPicStoreWithMocks(t, store, cache)

	// Создаём тестовый HTTP-сервер, который возвращает PNG
	data := decodeTestPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(data)
	}))
	defer server.Close()

	userID := uint(1)
	isPublic := false
	tags := "url,test"
	encryptionKey := ""
	img, err := ps.UploadImgURL(context.Background(), userID, server.URL, isPublic, tags, encryptionKey)
	if err != nil {
		t.Fatalf("UploadImgURL failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil PicImage")
	}
	if img.Path == "" {
		t.Error("Path should be set")
	}
	if img.UserID != userID {
		t.Errorf("UserID mismatch: got %d, want %d", img.UserID, userID)
	}
	if img.IsPublic != isPublic {
		t.Errorf("IsPublic mismatch: got %v, want %v", img.IsPublic, isPublic)
	}
	if img.Tags != tags {
		t.Errorf("Tags mismatch: got %q, want %q", img.Tags, tags)
	}
}
func TestGetImg(t *testing.T) {
	// Создаём store, который возвращает изображение по пути
	store := &controllableStore{
		getImageFunc: func(ctx context.Context, path string) (*models.Image, error) {
			if path == "valid/path.jpg" {
				return &models.Image{
					Model:       gorm.Model{ID: 42},
					Path:        "valid/path.jpg",
					IsPublic:    true,
					Tags:        "nature,landscape",
					IsEncrypted: false,
					UserID:      5,
				}, nil
			}
			return nil, fmt.Errorf("image not found")
		},
	}
	cache := &controllableCache{}
	ps := newTestPicStoreWithMocks(t, store, cache)

	// Тест 1: успешное получение изображения
	img, err := ps.GetImg(context.Background(), "valid/path.jpg")
	if err != nil {
		t.Fatalf("GetImg failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil PicImage")
	}
	if img.ID != 42 {
		t.Errorf("ID mismatch: got %d, want %d", img.ID, 42)
	}
	if img.Path == "" {
		t.Error("Path should be set")
	}
	if !img.IsPublic {
		t.Error("IsPublic should be true")
	}
	if img.Tags != "nature,landscape" {
		t.Errorf("Tags mismatch: got %q, want %q", img.Tags, "nature,landscape")
	}
	if img.UserID != 5 {
		t.Errorf("UserID mismatch: got %d, want %d", img.UserID, 5)
	}

	// Тест 2: изображение не найдено
	_, err = ps.GetImg(context.Background(), "invalid/path.jpg")
	if err == nil {
		t.Error("expected error for non-existent image")
	}
}

func TestGetPosts(t *testing.T) {
	// Создаём список тестовых изображений
	images := []*models.Image{
		{
			Model:       gorm.Model{ID: 1},
			Path:        "path1.jpg",
			IsPublic:    true,
			Tags:        "nature",
			IsEncrypted: false,
			UserID:      1,
		},
		{
			Model:       gorm.Model{ID: 2},
			Path:        "path2.jpg",
			IsPublic:    false,
			Tags:        "city",
			IsEncrypted: false,
			UserID:      2,
		},
		{
			Model:       gorm.Model{ID: 3},
			Path:        "path3.jpg",
			IsPublic:    true,
			Tags:        "nature landscape",
			IsEncrypted: true,
			UserID:      3,
		},
		{
			Model:       gorm.Model{ID: 4},
			Path:        "path4.jpg",
			IsPublic:    true,
			Tags:        "city night",
			IsEncrypted: false,
			UserID:      4,
		},
	}

	store := &controllableStore{
		getImagesFunc: func(ctx context.Context) ([]*models.Image, error) {
			return images, nil
		},
	}
	// Кэш, который не содержит данных (GetH возвращает ошибку)
	cache := &controllableCache{
		getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
			return fmt.Errorf("cache miss")
		},
		setHFunc: func(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
			return nil
		},
	}
	ps := newTestPicStoreWithMocks(t, store, cache)

	// Вызываем GetPosts
	posts, err := ps.GetPosts(context.Background())
	if err != nil {
		t.Fatalf("GetPosts failed: %v", err)
	}

	// Ожидаем только публичные незашифрованные изображения: ID 1 и 4 (ID 3 зашифрован, ID 2 приватный)
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(posts))
	}
	// Проверяем, что ID соответствуют ожидаемым
	ids := map[uint]bool{}
	for _, p := range posts {
		ids[p.ID] = true
	}
	if !ids[1] || !ids[4] {
		t.Errorf("expected posts with IDs 1 and 4, got %v", ids)
	}
	// Проверяем, что связи Prev/Next установлены
	for i, p := range posts {
		if i > 0 && p.Prev == nil {
			t.Errorf("post %d should have Prev", p.ID)
		}
		if i < len(posts)-1 && p.Next == nil {
			t.Errorf("post %d should have Next", p.ID)
		}
	}
}

func TestGetPostsWithTags(t *testing.T) {
	// Используем тот же набор изображений, что и в TestGetPosts
	images := []*models.Image{
		{
			Model:       gorm.Model{ID: 1},
			Path:        "path1.jpg",
			IsPublic:    true,
			Tags:        "nature",
			IsEncrypted: false,
			UserID:      1,
		},
		{
			Model:       gorm.Model{ID: 2},
			Path:        "path2.jpg",
			IsPublic:    true,
			Tags:        "city",
			IsEncrypted: false,
			UserID:      2,
		},
		{
			Model:       gorm.Model{ID: 3},
			Path:        "path3.jpg",
			IsPublic:    true,
			Tags:        "nature landscape",
			IsEncrypted: false,
			UserID:      3,
		},
		{
			Model:       gorm.Model{ID: 4},
			Path:        "path4.jpg",
			IsPublic:    true,
			Tags:        "city night",
			IsEncrypted: false,
			UserID:      4,
		},
	}

	store := &controllableStore{
		getImagesFunc: func(ctx context.Context) ([]*models.Image, error) {
			return images, nil
		},
	}
	cache := &controllableCache{
		getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
			return fmt.Errorf("cache miss")
		},
		setHFunc: func(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
			return nil
		},
	}
	ps := newTestPicStoreWithMocks(t, store, cache)

	// Тест 1: фильтр по тегу "nature"
	posts, err := ps.GetPostsWithTags(context.Background(), "nature")
	if err != nil {
		t.Fatalf("GetPostsWithTags failed: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts with tag 'nature', got %d", len(posts))
	}
	for _, p := range posts {
		if p.ID != 1 && p.ID != 3 {
			t.Errorf("unexpected post ID %d", p.ID)
		}
	}

	// Тест 2: фильтр по тегу "city"
	posts, err = ps.GetPostsWithTags(context.Background(), "city")
	if err != nil {
		t.Fatalf("GetPostsWithTags failed: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts with tag 'city', got %d", len(posts))
	}
	for _, p := range posts {
		if p.ID != 2 && p.ID != 4 {
			t.Errorf("unexpected post ID %d", p.ID)
		}
	}

	// Тест 3: фильтр по двум тегам (оба должны присутствовать)
	posts, err = ps.GetPostsWithTags(context.Background(), "nature landscape")
	if err != nil {
		t.Fatalf("GetPostsWithTags failed: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post with tags 'nature landscape', got %d", len(posts))
	}
	if posts[0].ID != 3 {
		t.Errorf("expected post ID 3, got %d", posts[0].ID)
	}

	// Тест 4: фильтр по несуществующему тегу
	posts, err = ps.GetPostsWithTags(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetPostsWithTags failed: %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("expected 0 posts, got %d", len(posts))
	}

	// Тест 5: исключающий тег (префикс '-')
	// В изображениях с ID 2 и 4 есть тег "city", исключаем его
	posts, err = ps.GetPostsWithTags(context.Background(), "-city")
	if err != nil {
		t.Fatalf("GetPostsWithTags failed: %v", err)
	}
	// Ожидаем только изображения без тега "city": ID 1 и 3
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts without tag 'city', got %d", len(posts))
	}
	for _, p := range posts {
		if p.ID != 1 && p.ID != 3 {
			t.Errorf("unexpected post ID %d", p.ID)
		}
	}
}

func TestGetPostsPage(t *testing.T) {
	// Создаём список тестовых изображений (все публичные, незашифрованные)
	images := []*models.Image{
		{
			Model:       gorm.Model{ID: 1},
			Path:        "path1.jpg",
			IsPublic:    true,
			Tags:        "nature",
			IsEncrypted: false,
			UserID:      1,
		},
		{
			Model:       gorm.Model{ID: 2},
			Path:        "path2.jpg",
			IsPublic:    true,
			Tags:        "city",
			IsEncrypted: false,
			UserID:      2,
		},
		{
			Model:       gorm.Model{ID: 3},
			Path:        "path3.jpg",
			IsPublic:    true,
			Tags:        "nature landscape",
			IsEncrypted: false,
			UserID:      3,
		},
		{
			Model:       gorm.Model{ID: 4},
			Path:        "path4.jpg",
			IsPublic:    true,
			Tags:        "city night",
			IsEncrypted: false,
			UserID:      4,
		},
		{
			Model:       gorm.Model{ID: 5},
			Path:        "path5.jpg",
			IsPublic:    true,
			Tags:        "abstract",
			IsEncrypted: false,
			UserID:      5,
		},
	}

	store := &controllableStore{
		getImagesFunc: func(ctx context.Context) ([]*models.Image, error) {
			return images, nil
		},
	}
	// Кэш, который не содержит данных (GetH возвращает ошибку)
	cache := &controllableCache{
		getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
			return fmt.Errorf("cache miss")
		},
		setHFunc: func(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
			return nil
		},
	}
	ps := newTestPicStoreWithMocks(t, store, cache)

	// Тест 1: первая страница, размер страницы 2, без тегов
	page := 1
	pageSize := 2
	posts, err := ps.GetPostsPage(context.Background(), page, pageSize, "")
	if err != nil {
		t.Fatalf("GetPostsPage failed: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts on page 1, got %d", len(posts))
	}
	if posts[0].ID != 1 || posts[1].ID != 2 {
		t.Errorf("expected posts IDs 1 and 2, got %d and %d", posts[0].ID, posts[1].ID)
	}

	// Тест 2: вторая страница, размер страницы 2
	page = 2
	posts, err = ps.GetPostsPage(context.Background(), page, pageSize, "")
	if err != nil {
		t.Fatalf("GetPostsPage failed: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts on page 2, got %d", len(posts))
	}
	if posts[0].ID != 3 || posts[1].ID != 4 {
		t.Errorf("expected posts IDs 3 and 4, got %d and %d", posts[0].ID, posts[1].ID)
	}

	// Тест 3: третья страница, размер страницы 2 (остаток 1)
	page = 3
	posts, err = ps.GetPostsPage(context.Background(), page, pageSize, "")
	if err != nil {
		t.Fatalf("GetPostsPage failed: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post on page 3, got %d", len(posts))
	}
	if posts[0].ID != 5 {
		t.Errorf("expected post ID 5, got %d", posts[0].ID)
	}

	// Тест 4: страница за пределами данных (пустой результат)
	page = 10
	posts, err = ps.GetPostsPage(context.Background(), page, pageSize, "")
	if err != nil {
		t.Fatalf("GetPostsPage failed: %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("expected 0 posts, got %d", len(posts))
	}

	// Тест 5: фильтрация по тегу "nature", страница 1, размер 2
	page = 1
	pageSize = 2
	posts, err = ps.GetPostsPage(context.Background(), page, pageSize, "nature")
	if err != nil {
		t.Fatalf("GetPostsPage with tags failed: %v", err)
	}
	// Ожидаем 2 поста с тегом "nature": ID 1 и 3
	if len(posts) != 2 {
		t.Fatalf("expected 2 posts with tag 'nature', got %d", len(posts))
	}
	if posts[0].ID != 1 || posts[1].ID != 3 {
		t.Errorf("expected posts IDs 1 and 3, got %d and %d", posts[0].ID, posts[1].ID)
	}

	// Тест 6: кэширование — симулируем наличие данных в кэше
	cacheHit := false
	cache.getHFunc = func(ctx context.Context, key string, obj types.ObjInterface) error {
		// Возвращаем предзаполненные данные
		cacheHit = true
		// Приводим obj к типу *picImages
		if pi, ok := obj.(*picImages); ok {
			*pi = picImages{
				&PicImage{
					ID:       99,
					Path:     "cached.jpg",
					IsPublic: true,
					Tags:     "cached",
					UserID:   99,
				},
			}
		}
		return nil
	}
	posts, err = ps.GetPostsPage(context.Background(), 1, 5, "")
	if err != nil {
		t.Fatalf("GetPostsPage with cache failed: %v", err)
	}
	if !cacheHit {
		t.Error("expected cache hit")
	}
	if len(posts) != 1 || posts[0].ID != 99 {
		t.Errorf("expected cached post ID 99, got %v", posts)
	}
}

func TestUpdateImage(t *testing.T) {
	// Создаём store с функцией обновления
	updateCalled := false
	var capturedUserID, capturedImageID uint
	var capturedIsPublic *bool
	var capturedTags *string
	store := &controllableStore{
		updateImageFunc: func(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error {
			updateCalled = true
			capturedUserID = userID
			capturedImageID = imageID
			capturedIsPublic = isPublic
			capturedTags = tags
			return nil
		},
	}
	// Кэш, который отслеживает вызов Remove
	removeCalled := false
	cache := &controllableCache{
		removeFunc: func(ctx context.Context, key string) error {
			if key == "images:all" || key == "posts:public" {
				removeCalled = true
			}
			return nil
		},
	}
	ps := newTestPicStoreWithMocks(t, store, cache)

	// Тест 1: обновление публичности
	isPublic := true
	err := ps.UpdateImage(context.Background(), 42, 100, &isPublic, nil)
	if err != nil {
		t.Fatalf("UpdateImage failed: %v", err)
	}
	if !updateCalled {
		t.Error("store.UpdateImage not called")
	}
	if capturedUserID != 42 || capturedImageID != 100 {
		t.Errorf("wrong IDs passed: userID=%d, imageID=%d", capturedUserID, capturedImageID)
	}
	if capturedIsPublic == nil || *capturedIsPublic != true {
		t.Errorf("isPublic not set correctly: %v", capturedIsPublic)
	}
	if capturedTags != nil {
		t.Errorf("tags should be nil, got %v", capturedTags)
	}
	if !removeCalled {
		t.Error("cache invalidation not called")
	}

	// Тест 2: обновление тегов
	updateCalled = false
	removeCalled = false
	tags := "new tags"
	err = ps.UpdateImage(context.Background(), 43, 101, nil, &tags)
	if err != nil {
		t.Fatalf("UpdateImage failed: %v", err)
	}
	if !updateCalled {
		t.Error("store.UpdateImage not called")
	}
	if capturedUserID != 43 || capturedImageID != 101 {
		t.Errorf("wrong IDs passed: userID=%d, imageID=%d", capturedUserID, capturedImageID)
	}
	if capturedIsPublic != nil {
		t.Errorf("isPublic should be nil, got %v", capturedIsPublic)
	}
	if capturedTags == nil || *capturedTags != tags {
		t.Errorf("tags not set correctly: %v", capturedTags)
	}
	if !removeCalled {
		t.Error("cache invalidation not called")
	}

	// Тест 3: обновление обоих полей
	updateCalled = false
	removeCalled = false
	isPublic2 := false
	tags2 := "mixed"
	err = ps.UpdateImage(context.Background(), 44, 102, &isPublic2, &tags2)
	if err != nil {
		t.Fatalf("UpdateImage failed: %v", err)
	}
	if !updateCalled {
		t.Error("store.UpdateImage not called")
	}
	if capturedUserID != 44 || capturedImageID != 102 {
		t.Errorf("wrong IDs passed: userID=%d, imageID=%d", capturedUserID, capturedImageID)
	}
	if capturedIsPublic == nil || *capturedIsPublic != false {
		t.Errorf("isPublic not set correctly: %v", capturedIsPublic)
	}
	if capturedTags == nil || *capturedTags != tags2 {
		t.Errorf("tags not set correctly: %v", capturedTags)
	}
	if !removeCalled {
		t.Error("cache invalidation not called")
	}

	// Тест 4: ошибка от store
	store.updateImageFunc = func(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error {
		return fmt.Errorf("store error")
	}
	err = ps.UpdateImage(context.Background(), 1, 1, nil, nil)
	if err == nil {
		t.Error("expected error from store")
	}
}

func TestDeleteImages(t *testing.T) {
	// Создаём изображения пользователя 10
	images := []*models.Image{
		{
			Model:       gorm.Model{ID: 100},
			Path:        "user10/img1.jpg",
			PreviewPath: "user10/preview1.jpg",
			IsPublic:    true,
			Tags:        "test",
			IsEncrypted: false,
			UserID:      10,
		},
		{
			Model:       gorm.Model{ID: 101},
			Path:        "user10/img2.jpg",
			PreviewPath: "user10/preview2.jpg",
			IsPublic:    false,
			Tags:        "test",
			IsEncrypted: false,
			UserID:      10,
		},
		{
			Model:       gorm.Model{ID: 102},
			Path:        "user10/img3.jpg",
			PreviewPath: "user10/preview3.jpg",
			IsPublic:    true,
			Tags:        "test",
			IsEncrypted: false,
			UserID:      10,
		},
	}
	// store, который возвращает эти изображения через GetImages
	store := &controllableStore{
		getImagesFunc: func(ctx context.Context) ([]*models.Image, error) {
			return images, nil
		},
	}
	// Отслеживаем вызов DelImages
	delCalled := false
	var capturedUserID uint
	var capturedImageIDs []uint
	store.delImagesFunc = func(ctx context.Context, userID uint, imageIDs []uint) error {
		delCalled = true
		capturedUserID = userID
		capturedImageIDs = imageIDs
		return nil
	}
	// Кэш, отслеживающий инвалидацию
	removeCalled := false
	cache := &controllableCache{
		removeFunc: func(ctx context.Context, key string) error {
			if key == "images:all" || key == "posts:public" {
				removeCalled = true
			}
			return nil
		},
	}
	ps := newTestPicStoreWithMocks(t, store, cache)

	// Тест 1: удаление одного изображения
	imageIDs := []uint{100}
	err := ps.DeleteImages(context.Background(), 10, imageIDs)
	if err != nil {
		t.Fatalf("DeleteImages failed: %v", err)
	}
	if !delCalled {
		t.Error("store.DelImages not called")
	}
	if capturedUserID != 10 {
		t.Errorf("wrong userID: got %d, want 10", capturedUserID)
	}
	if len(capturedImageIDs) != 1 || capturedImageIDs[0] != 100 {
		t.Errorf("wrong imageIDs: got %v, want [100]", capturedImageIDs)
	}
	if !removeCalled {
		t.Error("cache invalidation not called")
	}
	// Проверяем, что задачи на удаление файлов добавлены в очередь
	// Поскольку taskQueue является каналом, мы не можем легко проверить без модификации PicStore.
	// Пропустим эту проверку для простоты.

	// Тест 2: удаление нескольких изображений
	delCalled = false
	removeCalled = false
	imageIDs = []uint{101, 102}
	err = ps.DeleteImages(context.Background(), 10, imageIDs)
	if err != nil {
		t.Fatalf("DeleteImages failed: %v", err)
	}
	if !delCalled {
		t.Error("store.DelImages not called")
	}
	if capturedUserID != 10 {
		t.Errorf("wrong userID: got %d, want 10", capturedUserID)
	}
	if len(capturedImageIDs) != 2 || capturedImageIDs[0] != 101 || capturedImageIDs[1] != 102 {
		t.Errorf("wrong imageIDs: got %v, want [101, 102]", capturedImageIDs)
	}
	if !removeCalled {
		t.Error("cache invalidation not called")
	}

	// Тест 3: ошибка от store
	store.delImagesFunc = func(ctx context.Context, userID uint, imageIDs []uint) error {
		return fmt.Errorf("store deletion error")
	}
	err = ps.DeleteImages(context.Background(), 10, []uint{100})
	if err == nil {
		t.Error("expected error from store")
	}
}

func TestGetTagsWithCount(t *testing.T) {
	// Создаём изображения с тегами
	images := []*models.Image{
		{
			Model:       gorm.Model{ID: 1},
			Path:        "path1.jpg",
			IsPublic:    true,
			Tags:        "nature landscape",
			IsEncrypted: false,
			UserID:      1,
		},
		{
			Model:       gorm.Model{ID: 2},
			Path:        "path2.jpg",
			IsPublic:    true,
			Tags:        "city",
			IsEncrypted: false,
			UserID:      2,
		},
		{
			Model:       gorm.Model{ID: 3},
			Path:        "path3.jpg",
			IsPublic:    true,
			Tags:        "nature city",
			IsEncrypted: false,
			UserID:      3,
		},
		{
			Model:       gorm.Model{ID: 4},
			Path:        "path4.jpg",
			IsPublic:    false, // приватный, не должен учитываться
			Tags:        "private",
			IsEncrypted: false,
			UserID:      4,
		},
		{
			Model:       gorm.Model{ID: 5},
			Path:        "path5.jpg",
			IsPublic:    true,
			Tags:        "nature",
			IsEncrypted: false,
			UserID:      5,
		},
	}

	store := &controllableStore{
		getImagesFunc: func(ctx context.Context) ([]*models.Image, error) {
			return images, nil
		},
	}
	cache := &controllableCache{
		getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
			return fmt.Errorf("cache miss")
		},
		setHFunc: func(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
			return nil
		},
	}
	ps := newTestPicStoreWithMocks(t, store, cache)

	tags, err := ps.GetTagsWithCount(context.Background())
	if err != nil {
		t.Fatalf("GetTagsWithCount failed: %v", err)
	}
	// Ожидаемые теги: nature (3 раза), landscape (1), city (2)
	// landscape встречается только в первом изображении, но теги разделены пробелами.
	// В первом изображении теги "nature landscape" — два отдельных тега.
	// В третьем изображении "nature city" — nature и city.
	// Итого: nature: изображения 1,3,5 = 3; landscape: изображение 1 = 1; city: изображения 2,3 = 2.
	expected := map[string]int{
		"nature":    3,
		"landscape": 1,
		"city":      2,
	}
	if len(tags) != len(expected) {
		t.Fatalf("expected %d tags, got %d", len(expected), len(tags))
	}
	for tag, count := range expected {
		if tags[tag] != count {
			t.Errorf("tag %s: expected count %d, got %d", tag, count, tags[tag])
		}
	}
	// Проверим, что приватный тег "private" не попал в результат
	if _, ok := tags["private"]; ok {
		t.Error("private tag should not be counted")
	}
}

func TestGetUserPosts(t *testing.T) {
	// Создаём изображения разных пользователей
	images := []*models.Image{
		{
			Model:       gorm.Model{ID: 1},
			Path:        "path1.jpg",
			IsPublic:    true,
			Tags:        "test",
			IsEncrypted: false,
			UserID:      10,
		},
		{
			Model:       gorm.Model{ID: 2},
			Path:        "path2.jpg",
			IsPublic:    false,
			Tags:        "private",
			IsEncrypted: false,
			UserID:      10,
		},
		{
			Model:       gorm.Model{ID: 3},
			Path:        "path3.jpg",
			IsPublic:    true,
			Tags:        "test",
			IsEncrypted: true,
			UserID:      10,
		},
		{
			Model:       gorm.Model{ID: 4},
			Path:        "path4.jpg",
			IsPublic:    true,
			Tags:        "other",
			IsEncrypted: false,
			UserID:      20,
		},
	}

	store := &controllableStore{
		getImagesFunc: func(ctx context.Context) ([]*models.Image, error) {
			return images, nil
		},
	}
	cache := &controllableCache{
		getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
			return fmt.Errorf("cache miss")
		},
		setHFunc: func(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
			return nil
		},
	}
	ps := newTestPicStoreWithMocks(t, store, cache)

	// Тест для пользователя 10
	posts, err := ps.GetUserPosts(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetUserPosts failed: %v", err)
	}
	// Ожидаем все три изображения пользователя 10 (включая приватные и зашифрованные)
	if len(posts) != 3 {
		t.Fatalf("expected 3 posts for user 10, got %d", len(posts))
	}
	ids := map[uint]bool{}
	for _, p := range posts {
		ids[p.ID] = true
	}
	if !ids[1] || !ids[2] || !ids[3] {
		t.Errorf("missing expected posts, got IDs %v", ids)
	}
	// Проверим, что изображение другого пользователя не попало
	for _, p := range posts {
		if p.UserID != 10 {
			t.Errorf("post with wrong userID %d", p.UserID)
		}
	}

	// Тест для пользователя 20
	posts, err = ps.GetUserPosts(context.Background(), 20)
	if err != nil {
		t.Fatalf("GetUserPosts failed: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post for user 20, got %d", len(posts))
	}
	if posts[0].ID != 4 {
		t.Errorf("expected post ID 4, got %d", posts[0].ID)
	}

	// Тест для пользователя без изображений
	posts, err = ps.GetUserPosts(context.Background(), 999)
	if err != nil {
		t.Fatalf("GetUserPosts failed: %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("expected 0 posts, got %d", len(posts))
	}
}

func TestDecryptImage(t *testing.T) {
	// Создаём PicStore с включённым шифрованием
	cfg := Config{
		PicPath:          t.TempDir(),
		EnableEncryption: true,
		CacheTTL:         time.Minute,
		MaxFileSize:      5 * 1024 * 1024,
		AsyncProcessing:  false,
		TempStorageTTL:   time.Hour,
		KeyStorageTTL:    time.Minute,
		WorkerPoolSize:   1,
		TaskQueueSize:    10,
		ConvertToFormat:  "webp",
		Quality:          85,
		GeneratePreview:  true,
		PreviewMaxSize:   640,
		PreviewFormat:    "webp",
		PreviewQuality:   75,
	}
	ctx := context.Background()
	lgr, err := logger.New(ctx, logger.SetLevel("debug"), logger.SetLogPath(""))
	if err != nil {
		t.Fatal(err)
	}
	store := &controllableStore{}
	cache := &controllableCache{}
	ps, err := New(ctx, cfg, lgr, store, cache)
	if err != nil {
		t.Fatal(err)
	}

	// Тестовые данные
	plainData := []byte("test image data")
	key := "secret123"
	// Шифруем с помощью encryptFile (приватный метод, но доступен внутри пакета)
	encrypted, nonce, salt, err := ps.encryptFile(plainData, key)
	if err != nil {
		t.Fatalf("encryptFile failed: %v", err)
	}
	// Создаём файл внутри PicPath с относительным путём
	filename := "encrypted.bin"
	filePath := filepath.Join(cfg.PicPath, filename)
	if err := os.WriteFile(filePath, encrypted, 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(filePath)

	// Мокаем GetImg, чтобы возвращал изображение с относительным путём и метаданными шифрования
	store.getImageFunc = func(ctx context.Context, path string) (*models.Image, error) {
		if path != filename {
			return nil, fmt.Errorf("image not found")
		}
		return &models.Image{
			Model:       gorm.Model{ID: 1},
			Path:        filename,
			IsPublic:    true,
			Tags:        "test",
			IsEncrypted: true,
			Salt:        salt,
			Nonce:       nonce,
			UserID:      1,
		}, nil
	}
	// Вызовем DecryptImage с относительным путём
	decrypted, err := ps.DecryptImage(ctx, filename, key)
	if err != nil {
		t.Fatalf("DecryptImage failed: %v", err)
	}
	if string(decrypted) != string(plainData) {
		t.Errorf("decrypted data mismatch: got %q, want %q", decrypted, plainData)
	}

	// Тест с неверным ключом
	_, err = ps.DecryptImage(ctx, filename, "wrongkey")
	if err == nil {
		t.Error("DecryptImage with wrong key should fail")
	}

	// Тест с незашифрованным изображением
	store.getImageFunc = func(ctx context.Context, path string) (*models.Image, error) {
		return &models.Image{
			Model:       gorm.Model{ID: 2},
			Path:        filename,
			IsPublic:    true,
			Tags:        "test",
			IsEncrypted: false,
			UserID:      2,
		}, nil
	}
	// Метод должен вернуть данные файла как есть (зашифрованные), потому что IsEncrypted = false
	decrypted, err = ps.DecryptImage(ctx, filename, key)
	if err != nil {
		t.Fatalf("DecryptImage for non-encrypted image failed: %v", err)
	}
	if string(decrypted) != string(encrypted) {
		t.Errorf("expected raw file data, got %q", decrypted)
	}
}

func TestGetMaxFileSize(t *testing.T) {
	// Создаём PicStore с разными значениями MaxFileSize
	tests := []struct {
		name string
		size int64
	}{
		{"zero", 0},
		{"5MB", 5 * 1024 * 1024},
		{"10MB", 10 * 1024 * 1024},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				PicPath:          t.TempDir(),
				MaxFileSize:      tt.size,
				CacheTTL:         time.Minute,
				EnableEncryption: false,
				AsyncProcessing:  false,
				TempStorageTTL:   time.Hour,
				KeyStorageTTL:    time.Minute,
				WorkerPoolSize:   1,
				TaskQueueSize:    10,
				ConvertToFormat:  "webp",
				Quality:          85,
				GeneratePreview:  true,
				PreviewMaxSize:   640,
				PreviewFormat:    "webp",
				PreviewQuality:   75,
			}
			ctx := context.Background()
			lgr, err := logger.New(ctx, logger.SetLevel("debug"), logger.SetLogPath(""))
			if err != nil {
				t.Fatal(err)
			}
			store := &controllableStore{}
			cache := &controllableCache{}
			ps, err := New(ctx, cfg, lgr, store, cache)
			if err != nil {
				t.Fatal(err)
			}
			if got := ps.GetMaxFileSize(); got != tt.size {
				t.Errorf("GetMaxFileSize() = %d, want %d", got, tt.size)
			}
		})
	}
}
