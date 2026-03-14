package redisdb

type Config struct {
	Address  string `env:"REDIS_ADDRESS" envDefault:"localhost:6379"`
	Password string `env:"REDIS_PASSWORD" envDefault:""`
	DB       int    `env:"REDIS_DB" envDefault:"0"`
}
