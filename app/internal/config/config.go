package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

type Config struct {
	Listen struct {
		Type   string
		Port   string
		BindIP string
	}
	Postgres struct {
		Host     string
		Port     string
		Database string
		Username string
		Password string
	}
	Cassandra struct {
		Host     string // хост (например "cassandra" в docker-compose)
		Keyspace string // "todolist"
	}
	Redis struct {
		Host     string
		Port     string
		Password string
	}
	Gateway struct {
		Host   string
		Port   string
		BindIP string
	}
	SMTP struct {
		Host         string
		Port         string
		Username     string
		Password     string
		SupportEmail string
		AppURL       string
	}

	JWTSecret                 string
	GatewaySign               string
	Env                       string
	SubscriptionExpiryWindows []string
	ExpiryCheckInterval       time.Duration
}

func GetConfig() (*Config, error) {
	cfg := &Config{}

	required := []string{
		"DB_USERS_HOST",
		"DB_USERS_PORT",
		"DB_USERS_DATABASE",
		"DB_USERS_USERNAME",
		"DB_USERS_PASSWORD",
		"JWT_SECRET",
		"GATEWAY_SIGN",
		"SUBSCRIPTION_EXPIRY_WINDOWS",
		"EXPIRY_CHECK_INTERVAL",
	}

	for _, key := range required {
		if os.Getenv(key) == "" {
			return nil, errors.New("missing required env variable: " + key)
		}
	}

	cfg.Listen.Port = os.Getenv("USERS_PORT")
	cfg.Listen.BindIP = os.Getenv("LISTEN_BIND_IP")
	cfg.Listen.Type = "port"

	cfg.Postgres.Host = os.Getenv("DB_USERS_HOST")
	cfg.Postgres.Port = os.Getenv("DB_USERS_PORT")
	cfg.Postgres.Database = os.Getenv("DB_USERS_DATABASE")
	cfg.Postgres.Username = os.Getenv("DB_USERS_USERNAME")
	cfg.Postgres.Password = os.Getenv("DB_USERS_PASSWORD")

	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	cfg.GatewaySign = os.Getenv("GATEWAY_SIGN")

	windows := os.Getenv("SUBSCRIPTION_EXPIRY_WINDOWS")
	for _, window := range strings.Split(windows, ",") {
		cfg.SubscriptionExpiryWindows = append(cfg.SubscriptionExpiryWindows, window)
	}
	// env: SUBSCRIPTION_EXPIRY_WINDOWS, например "7d,3d,1d,3h"
	d, _ := time.ParseDuration(os.Getenv("EXPIRY_CHECK_INTERVAL"))
	cfg.ExpiryCheckInterval = d

	cfg.Gateway.Host = os.Getenv("GATEWAY_HOST")
	cfg.Gateway.Port = os.Getenv("GATEWAY_PORT")

	cfg.SMTP.Host = os.Getenv("SMTP_HOST")
	cfg.SMTP.Port = os.Getenv("SMTP_PORT")
	cfg.SMTP.Username = os.Getenv("SMTP_USERNAME")
	// App Password от Gmail хранится в .env с дефисами вместо пробелов
	cfg.SMTP.Password = strings.ReplaceAll(os.Getenv("SMTP_PASSWORD"), "-", " ")
	cfg.SMTP.SupportEmail = os.Getenv("SUPPORT_EMAIL")
	cfg.SMTP.AppURL = os.Getenv("APP_URL")

	cfg.Cassandra.Host = os.Getenv("CASSANDRA_HOST")
	if cfg.Cassandra.Host == "" {
		cfg.Cassandra.Host = "cassandra" // дефолт для docker-compose
	}
	cfg.Cassandra.Keyspace = os.Getenv("CASSANDRA_KEYSPACE")
	if cfg.Cassandra.Keyspace == "" {
		cfg.Cassandra.Keyspace = "todolist"
	}

	cfg.Redis.Host = os.Getenv("REDIS_AUTH_HOST")
	if cfg.Redis.Host == "" {
		cfg.Redis.Host = "redis-auth"
	}
	cfg.Redis.Port = os.Getenv("REDIS_AUTH_PORT")
	if cfg.Redis.Port == "" {
		cfg.Redis.Port = "6379"
	}
	cfg.Redis.Password = os.Getenv("REDIS_AUTH_PASSWORD")

	return cfg, nil
}
