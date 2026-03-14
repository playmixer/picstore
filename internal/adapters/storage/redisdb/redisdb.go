package redisdb

import (
	"context"
	"picstore/internal/adapters/storage/types"
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
