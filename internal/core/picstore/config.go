package picstore

import "time"

type Config struct {
	PicPath          string        `env:"PIC_PATH" envDefault:"./tmp"`
	CacheTTL         time.Duration `env:"CACHE_TTL" envDefault:"1m"`
	EnableEncryption bool          `env:"ENABLE_ENCRYPTION" envDefault:"false"`
	MaxFileSize      int64         `env:"MAX_FILE_SIZE" envDefault:"5242880"` // 5 MB in bytes
}
