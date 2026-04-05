package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
)

// ClientRedis интерфейс для работы с Redis
type ClientRedis interface {
	Save(ctx context.Context, key string, value interface{}) error
	Get(ctx context.Context, key string) (map[string]string, error)
	Delete(ctx context.Context, key string) error
}

// redisClient структура, реализующая интерфейс ClientRedis
type redisClient struct {
	client *redis.Client
}

// NewClient создает новый клиент Redis
func NewClient(ctx context.Context, options *redis.Options) (ClientRedis, error) {
	client := redis.NewClient(options)
	// Проверка соединения
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &redisClient{client: client}, nil
}

// Save сохраняет значение в Redis с указанным ключом и временем истечения
func (r *redisClient) Save(ctx context.Context, key string, value interface{}) error {
	fields, err := structToMap(value)
	if err != nil {
		return err
	}

	// Сохраняем данные в хеш
	if err = r.client.HSet(ctx, key, fields).Err(); err != nil {
		return err
	}

	// Устанавливаем время истечения в 10 минут
	return r.client.Expire(ctx, key, 10*time.Minute).Err()
}

// Get извлекает значение из Redis по ключу
func (r *redisClient) Get(ctx context.Context, key string) (map[string]string, error) {
	data, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Delete удаляет значение из Redis по ключу
func (r *redisClient) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// Утилита для преобразования структуры в карту
func structToMap(value interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	var fields map[string]interface{}
	if err = json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}
