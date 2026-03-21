package picstore

import (
	"context"
	"fmt"
	"time"

	"github.com/playmixer/single-auth/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// ViewTrackerConfig конфигурация для трекера просмотров.
type ViewTrackerConfig struct {
	// CooldownPeriod определяет минимальный интервал между учётами просмотров от одного пользователя для одного изображения.
	CooldownPeriod time.Duration `env:"VIEW_COOLDOWN" envDefault:"1h"`
	// SyncInterval определяет, как часто накопленные в Redis просмотры синхронизируются в PostgreSQL.
	SyncInterval time.Duration `env:"VIEW_SYNC_INTERVAL" envDefault:"5m"`
	// RedisKeyPrefix префикс для ключей Redis, используемых для хранения счётчиков и блокировок.
	RedisKeyPrefix string `env:"VIEW_REDIS_PREFIX" envDefault:"views:"`
}

// ViewTracker отвечает за учёт просмотров изображений с защитой от накрутки и периодической синхронизацией в БД.
type ViewTracker struct {
	store  store
	cache  cache
	log    *logger.Logger
	cfg    ViewTrackerConfig
	stopCh chan struct{}
}

// NewViewTracker создаёт новый экземпляр ViewTracker.
func NewViewTracker(store store, cache cache, log *logger.Logger, cfg ViewTrackerConfig) *ViewTracker {
	return &ViewTracker{
		store:  store,
		cache:  cache,
		log:    log,
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

// Start запускает фоновую синхронизацию просмотров.
func (vt *ViewTracker) Start(ctx context.Context) {
	go vt.syncLoop(ctx)
}

// Stop останавливает фоновую синхронизацию.
func (vt *ViewTracker) Stop() {
	close(vt.stopCh)
}

// RecordView пытается учесть просмотр изображения imageID пользователем userID.
// Возвращает true, если просмотр засчитан (прошло больше CooldownPeriod с предыдущего учёта),
// и false, если просмотр не засчитан (например, пользователь уже смотрел недавно).
// В случае ошибки возвращает false и ошибку.
func (vt *ViewTracker) RecordView(ctx context.Context, imageID uint, userID uint) (bool, error) {
	// Ключ блокировки: views:cooldown:{imageID}:{userID}
	cooldownKey := fmt.Sprintf("%scooldown:%d:%d", vt.cfg.RedisKeyPrefix, imageID, userID)
	// Пытаемся установить ключ с TTL = CooldownPeriod, если его ещё нет (SetNX)
	ok, err := vt.cache.SetNX(ctx, cooldownKey, []byte("1"), vt.cfg.CooldownPeriod)
	if err != nil {
		vt.log.Error("failed to set cooldown key", zap.Uint("imageID", imageID), zap.Uint("userID", userID), zap.Error(err))
		return false, fmt.Errorf("set cooldown: %w", err)
	}
	if !ok {
		// Ключ уже существует — пользователь недавно уже смотрел это изображение
		return false, nil
	}

	// Ключ счётчика: views:counter:{imageID}
	counterKey := fmt.Sprintf("%scounter:%d", vt.cfg.RedisKeyPrefix, imageID)
	// Увеличиваем счётчик в Redis
	newVal, err := vt.cache.Incr(ctx, counterKey)
	if err != nil {
		vt.log.Error("failed to increment view counter", zap.Uint("imageID", imageID), zap.Error(err))
		// Откатываем блокировку? Можно удалить cooldownKey, но это сложно.
		// Лучше оставить, чтобы пользователь не мог накручивать из-за ошибок.
		return false, fmt.Errorf("incr counter: %w", err)
	}
	vt.log.Debug("view recorded", zap.Uint("imageID", imageID), zap.Uint("userID", userID), zap.Int64("newCount", newVal))
	return true, nil
}

// syncLoop периодически синхронизирует накопленные в Redis счётчики в PostgreSQL.
func (vt *ViewTracker) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(vt.cfg.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := vt.syncViews(ctx); err != nil {
				vt.log.Error("failed to sync views", zap.Error(err))
			}
		case <-vt.stopCh:
			vt.log.Info("view tracker sync loop stopped")
			return
		case <-ctx.Done():
			vt.log.Info("view tracker sync loop cancelled")
			return
		}
	}
}

// syncViews находит все ключи счётчиков в Redis, обновляет соответствующие записи в БД и сбрасывает счётчики.
func (vt *ViewTracker) syncViews(ctx context.Context) error {
	// Паттерн для поиска ключей счётчиков
	pattern := fmt.Sprintf("%scounter:*", vt.cfg.RedisKeyPrefix)
	keys, err := vt.cache.Keys(ctx, pattern)
	if err != nil {
		return fmt.Errorf("failed to get counter keys: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}

	vt.log.Debug("syncing views", zap.Int("counterKeys", len(keys)))

	// Для каждого ключа получаем текущее значение и сбрасываем его
	for _, key := range keys {
		// Извлекаем imageID из ключа (формат views:counter:{imageID})
		var imageID uint
		_, err := fmt.Sscanf(key, vt.cfg.RedisKeyPrefix+"counter:%d", &imageID)
		if err != nil {
			vt.log.Warn("invalid counter key format", zap.String("key", key), zap.Error(err))
			continue
		}

		// Получаем текущее значение счётчика как uint64
		count, err := vt.cache.GetUint64(ctx, key)
		if err != nil {
			// Если ключ исчез (например, истёк TTL или был удалён), просто пропускаем
			if err == redis.Nil {
				continue
			}
			vt.log.Warn("failed to get counter value", zap.String("key", key), zap.Error(err))
			continue
		}

		// Обновляем поле views в БД
		if count > 0 {
			if err := vt.store.IncrementViews(ctx, imageID, uint(count)); err != nil {
				vt.log.Error("failed to increment views in database", zap.Uint("imageID", imageID), zap.Uint64("count", count), zap.Error(err))
				// Не удаляем ключ, чтобы повторить попытку в следующей синхронизации
				continue
			}
			vt.log.Debug("views incremented in database", zap.Uint("imageID", imageID), zap.Uint64("count", count))
		}

		// Удаляем ключ, чтобы сбросить счётчик
		if err := vt.cache.Remove(ctx, key); err != nil {
			vt.log.Warn("failed to delete counter key", zap.String("key", key), zap.Error(err))
		}
	}
	return nil
}
