package wsticket

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "mini-hermes:ws-ticket:"

type RedisRepository struct {
	client redis.Cmdable
}

func NewRedisRepository(client redis.Cmdable) *RedisRepository {
	return &RedisRepository{client: client}
}

func (r *RedisRepository) Store(ctx context.Context, ticket, userID string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, keyPrefix+ticket, userID, ttl).Result()
}

// GETDEL removes the ticket in the same Redis operation that returns its owner.
func (r *RedisRepository) Consume(ctx context.Context, ticket string) (string, error) {
	userID, err := r.client.GetDel(ctx, keyPrefix+ticket).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrInvalidTicket
	}
	return userID, err
}
