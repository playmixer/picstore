package picstore

import (
	"context"
	"encoding/base64"
	"picstore/internal/adapters/models"
	"picstore/internal/adapters/storage/types"
	"testing"
	"time"

	"github.com/playmixer/single-auth/pkg/logger"
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
