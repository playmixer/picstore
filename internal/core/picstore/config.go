package picstore

import "time"

// Config содержит все настраиваемые параметры PicStore.
// Значения по умолчанию задаются через теги env (используется godotenv).
type Config struct {
	// PicPath — путь к директории, где хранятся загруженные изображения.
	PicPath string `env:"PIC_PATH" envDefault:"./tmp"`

	// CacheTTL — время жизни записей в кэше Redis.
	CacheTTL time.Duration `env:"CACHE_TTL" envDefault:"1m"`

	// EnableEncryption — разрешить шифрование изображений при загрузке.
	EnableEncryption bool `env:"ENABLE_ENCRYPTION" envDefault:"false"`

	// MaxFileSize — максимальный размер загружаемого файла в байтах.
	MaxFileSize int64 `env:"MAX_FILE_SIZE" envDefault:"5242880"` // 5 MB in bytes

	// Настройки асинхронной обработки

	// AsyncProcessing — включить асинхронную обработку изображений (конвертация, превью, шифрование).
	AsyncProcessing bool `env:"ASYNC_PROCESSING" envDefault:"true"`

	// TempStorageTTL — время хранения временных файлов до их обработки.
	TempStorageTTL time.Duration `env:"TEMP_STORAGE_TTL" envDefault:"24h"`

	// KeyStorageTTL — время хранения ключей шифрования в кэше.
	KeyStorageTTL time.Duration `env:"KEY_STORAGE_TTL" envDefault:"10m"`

	// WorkerPoolSize — количество воркеров для обработки изображений.
	WorkerPoolSize int `env:"WORKER_POOL_SIZE" envDefault:"5"`

	// TaskQueueSize — размер очереди задач для асинхронной обработки.
	TaskQueueSize int `env:"TASK_QUEUE_SIZE" envDefault:"1000"`

	// Настройки конвертации изображений

	// ConvertToFormat — целевой формат конвертации (webp, jpeg, png). Пустая строка отключает конвертацию.
	ConvertToFormat string `env:"CONVERT_TO_FORMAT" envDefault:"webp"`

	// Quality — качество конвертации (1–100). Применяется для lossy‑форматов (WebP, JPEG).
	Quality int `env:"CONVERT_QUALITY" envDefault:"85"`

	// Настройки превью

	// GeneratePreview — генерировать превью для изображений.
	GeneratePreview bool `env:"GENERATE_PREVIEW" envDefault:"true"`

	// PreviewMaxSize — максимальный размер большей стороны превью в пикселях.
	PreviewMaxSize int `env:"PREVIEW_MAX_SIZE" envDefault:"640"`

	// PreviewFormat — формат превью (webp, jpeg, png).
	PreviewFormat string `env:"PREVIEW_FORMAT" envDefault:"webp"`

	// PreviewQuality — качество превью (1–100).
	PreviewQuality int `env:"PREVIEW_QUALITY" envDefault:"75"`

	// Настройки статистики просмотров

	// ViewCooldownPeriod — минимальный интервал между засчитыванием повторных просмотров от одного пользователя.
	ViewCooldownPeriod time.Duration `env:"VIEW_COOLDOWN" envDefault:"1h"`

	// ViewSyncInterval — интервал синхронизации счётчиков просмотров из Redis в БД.
	ViewSyncInterval time.Duration `env:"VIEW_SYNC_INTERVAL" envDefault:"5m"`

	// ViewRedisPrefix — префикс ключей Redis для хранения временных счётчиков просмотров.
	ViewRedisPrefix string `env:"VIEW_REDIS_PREFIX" envDefault:"views:"`
}
