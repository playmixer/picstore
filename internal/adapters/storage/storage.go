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

	NewImage(ctx context.Context, userID uint, path string, isPublic bool, tags string) (*models.Image, error)
	DelImage(ctx context.Context, userID uint, imageID uint) error
	GetImage(ctx context.Context, path string) (*models.Image, error)
	GetImages(ctx context.Context) ([]*models.Image, error)
	UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error

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
}

func NewCache(cfg ConfigCache) (Cache, error) {
	return redisdb.New(cfg.Redis), nil
}
