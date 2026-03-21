package picstore

import "time"

type Config struct {
	PicPath          string        `env:"PIC_PATH" envDefault:"./tmp"`
	CacheTTL         time.Duration `env:"CACHE_TTL" envDefault:"1m"`
	EnableEncryption bool          `env:"ENABLE_ENCRYPTION" envDefault:"false"`
	MaxFileSize      int64         `env:"MAX_FILE_SIZE" envDefault:"5242880"` // 5 MB in bytes

	// Настройки асинхронной обработки
	AsyncProcessing bool          `env:"ASYNC_PROCESSING" envDefault:"true"`
	TempStorageTTL  time.Duration `env:"TEMP_STORAGE_TTL" envDefault:"24h"`
	KeyStorageTTL   time.Duration `env:"KEY_STORAGE_TTL" envDefault:"10m"`
	WorkerPoolSize  int           `env:"WORKER_POOL_SIZE" envDefault:"5"`
	TaskQueueSize   int           `env:"TASK_QUEUE_SIZE" envDefault:"1000"`

	// Настройки конвертации изображений
	ConvertToFormat string `env:"CONVERT_TO_FORMAT" envDefault:"webp"` // webp, jpeg, png, "" - без конвертации
	Quality         int    `env:"CONVERT_QUALITY" envDefault:"85"`     // качество от 1 до 100

	// Настройки превью
	GeneratePreview bool   `env:"GENERATE_PREVIEW" envDefault:"true"`
	PreviewMaxSize  int    `env:"PREVIEW_MAX_SIZE" envDefault:"640"` // максимальный размер по большей стороне
	PreviewFormat   string `env:"PREVIEW_FORMAT" envDefault:"webp"`  // формат превью
	PreviewQuality  int    `env:"PREVIEW_QUALITY" envDefault:"75"`   // качество превью

	// Настройки статистики просмотров
	ViewCooldownPeriod time.Duration `env:"VIEW_COOLDOWN" envDefault:"1h"`
	ViewSyncInterval   time.Duration `env:"VIEW_SYNC_INTERVAL" envDefault:"5m"`
	ViewRedisPrefix    string        `env:"VIEW_REDIS_PREFIX" envDefault:"views:"`
}
