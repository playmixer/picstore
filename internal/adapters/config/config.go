package config

import (
	"fmt"
	"picstore/internal/adapters/api/rest"
	"picstore/internal/adapters/storage"
	"picstore/internal/adapters/storage/database"
	"picstore/internal/core/picstore"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config конфигурация сервиса.
type Config struct {
	API       rest.Config
	Pic       picstore.Config
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogDir    string `env:"LOG_DIR" envDefault:"./logs"`
	SecretKey string `env:"AUTH_SECRET_KEY"`
	Store     storage.Config
	Cache     storage.ConfigCache
}

// Init инициализирует конфигурацию сервиса.
func Init() (*Config, error) {
	cfg := Config{
		API: rest.Config{},
		Pic: picstore.Config{},
		Store: storage.Config{
			Database: database.Config{},
		},
		Cache: storage.ConfigCache{},
	}

	_ = godotenv.Load(".env")

	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("error parse config %w", err)
	}

	return &cfg, nil
}
