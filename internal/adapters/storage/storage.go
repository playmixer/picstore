package storage

import (
	"context"
	"fmt"
	"picstore/internal/adapters/models"
	"picstore/internal/adapters/storage/database"
	"picstore/internal/adapters/storage/redisdb"
	"picstore/internal/adapters/storage/types"
	"time"
)

type Config struct {
	TypeStorage string `env:"AUTH_STORAGE" envDefault:"database"`
	Database    database.Config
}

type Storage interface {
	//Auth
	GetUser(ctx context.Context, username string) (*models.User, error)
	CreateUser(ctx context.Context, login, email, passwordHash string, source string, admin bool) (*models.User, error)
	GetUserByID(ctx context.Context, userID uint) (*models.User, error)
	UpdUser(ctx context.Context, user *models.User) error
	GetUserByEmail(ctx context.Context, email string) (user *models.User, err error)

	NewRole(ctx context.Context, name string) (*models.Role, error)
	GetRole(ctx context.Context, name string) (*models.Role, error)

	// Основные методы для изображений
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
	IncrementViews(ctx context.Context, imageID uint, delta uint) error
	GetPostsPage(ctx context.Context, userID uint, isPublic bool, includeTags, excludeTags []string, limit, offset int) ([]*models.Image, int64, error)
	GetFilteredImageIDs(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error)

	Close() error
}

func New(cfg Config) (Storage, error) {
	store, err := database.New(cfg.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed init databse storage: %w", err)
	}
	return store, nil
}

type ConfigCache struct {
	Redis redisdb.Config
}

type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	GetH(ctx context.Context, key string, obj types.ObjInterface) (err error)
	SetH(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error
	Remove(ctx context.Context, key string) error
	Incr(ctx context.Context, key string) (int64, error)
	IncrBy(ctx context.Context, key string, delta int64) (int64, error)
	SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	Keys(ctx context.Context, pattern string) ([]string, error)
	GetUint64(ctx context.Context, key string) (uint64, error)
	SetUint64(ctx context.Context, key string, value uint64, ttl time.Duration) error
}

func NewCache(cfg ConfigCache) (Cache, error) {
	return redisdb.New(cfg.Redis), nil
}
