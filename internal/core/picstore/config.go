package picstore

import "time"

type Config struct {
	PicPath  string        `env:"PIC_PATH" envDefault:"./tmp"`
	CacheTTL time.Duration `env:"CACHE_TTL" envDefault:"1m"`
}
