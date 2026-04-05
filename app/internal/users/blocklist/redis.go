package blocklist

import (
	"context"

	"github.com/go-redis/redis/v8"
)

const keyPrefix = "blocked:"

// Repository — хранилище заблокированных пользователей в Redis.
type Repository interface {
	Block(ctx context.Context, userID string) error
	Unblock(ctx context.Context, userID string) error
}

type redisRepo struct {
	client *redis.Client
}

// NewRepository создаёт Redis-репозиторий blocklist.
func NewRepository(client *redis.Client) Repository {
	return &redisRepo{client: client}
}

func (r *redisRepo) Block(ctx context.Context, userID string) error {
	// 0 = без TTL: блок действует до явного снятия
	return r.client.Set(ctx, keyPrefix+userID, "1", 0).Err()
}

func (r *redisRepo) Unblock(ctx context.Context, userID string) error {
	return r.client.Del(ctx, keyPrefix+userID).Err()
}
