package redisdb

import (
	"context"
	"picstore/internal/adapters/storage/types"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisDB struct {
	Client *redis.Client
}

func New(cfg Config) *RedisDB {
	r := &RedisDB{
		Client: redis.NewClient(&redis.Options{
			Addr:     cfg.Address,
			Password: cfg.Password,
			DB:       cfg.DB,
		}),
	}

	return r
}

func (r *RedisDB) Get(ctx context.Context, key string) ([]byte, error) {
	return r.Client.Get(ctx, key).Bytes()
}

func (r *RedisDB) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.Client.Set(ctx, key, value, ttl).Err()
}

func (r *RedisDB) GetH(ctx context.Context, key string, obj types.ObjInterface) (err error) {
	return r.Client.Get(ctx, key).Scan(obj)
}

func (r *RedisDB) SetH(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
	return r.Client.Set(ctx, key, value, ttl).Err()
}

func (r *RedisDB) Remove(ctx context.Context, key string) error {
	return r.Client.Del(ctx, key).Err()
}

func (r *RedisDB) Incr(ctx context.Context, key string) (int64, error) {
	return r.Client.Incr(ctx, key).Result()
}

func (r *RedisDB) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	return r.Client.SetNX(ctx, key, value, ttl).Result()
}

func (r *RedisDB) Keys(ctx context.Context, pattern string) ([]string, error) {
	return r.Client.Keys(ctx, pattern).Result()
}

func (r *RedisDB) IncrBy(ctx context.Context, key string, delta int64) (int64, error) {
	return r.Client.IncrBy(ctx, key, delta).Result()
}

func (r *RedisDB) GetUint64(ctx context.Context, key string) (uint64, error) {
	val, err := r.Client.Get(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	// Преобразуем строку в uint64
	return strconv.ParseUint(val, 10, 64)
}
