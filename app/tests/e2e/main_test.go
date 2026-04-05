//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"TODOLIST/app/internal/config"
	usersHandler "TODOLIST/app/internal/users"
	"TODOLIST/app/internal/users/blocklist"
	postgresRepo "TODOLIST/app/internal/users/db/postgresql"
	"TODOLIST/app/internal/users/model"
	userSender "TODOLIST/app/internal/users/sender"
	userService "TODOLIST/app/internal/users/service"
	"TODOLIST/app/internal/users/storagePostgres"
	wsHub "TODOLIST/app/internal/users/ws"
	notification "TODOLIST/app/notify"
	"TODOLIST/app/pkg/client/postgresql"

	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/julienschmidt/httprouter"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

const (
	testGatewaySign = "e2e-test-gateway-sign-secret"
	testUserID      = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	testUserID2     = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

var testServer *httptest.Server

// noopCassandraRepo — заглушка Cassandra: позволяет тестировать user/friend логику без Cassandra.
type noopCassandraRepo struct{}

var _ storagePostgres.CassandraRepository = (*noopCassandraRepo)(nil)

func (n *noopCassandraRepo) CreateMessage(_ context.Context, _ model.Message) error { return nil }
func (n *noopCassandraRepo) GetMessages(_ context.Context, _, _, _ string, _ int) ([]model.Message, error) {
	return []model.Message{}, nil
}
func (n *noopCassandraRepo) MarkMessagesRead(_ context.Context, _, _ string) error { return nil }
func (n *noopCassandraRepo) GetUnreadCount(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (n *noopCassandraRepo) EditMessage(_ context.Context, _, _, _ string) (*model.Message, error) {
	return nil, fmt.Errorf("not available in e2e stub")
}
func (n *noopCassandraRepo) DeleteMessage(_ context.Context, _, _ string) (string, string, error) {
	return "", "", fmt.Errorf("not available in e2e stub")
}
func (n *noopCassandraRepo) GetUnreadCountsPerFriend(_ context.Context, _ string) (map[string]int, error) {
	return map[string]int{}, nil
}
func (n *noopCassandraRepo) SaveMessageHistory(_ context.Context, _, _, _ string) error { return nil }
func (n *noopCassandraRepo) GetMessageHistory(_ context.Context, _ string) ([]model.MessageHistoryEntry, error) {
	return []model.MessageHistoryEntry{}, nil
}

// noopNotificationClient — заглушка внешних уведомлений (gateway/tasks-service).
type noopNotificationClient struct{}

var _ notification.Client = (*noopNotificationClient)(nil)

func (n *noopNotificationClient) SendExpiryNotification(_ context.Context, _ model.UserTODO, _ string) error {
	return nil
}
func (n *noopNotificationClient) SyncSubscription(_ context.Context, _ notification.SubscriptionSyncRequest) error {
	return nil
}
func (n *noopNotificationClient) SendFriendRequestNotification(_ string, _ int64, _ string) {}

// noopEmailSender — заглушка email отправки.
type noopEmailSender struct{}

var _ userSender.EmailSender = (*noopEmailSender)(nil)

func (n *noopEmailSender) SendPasswordResetCode(_, _ string) error { return nil }

func TestMain(m *testing.M) {
	ctx := context.Background()

	// --- PostgreSQL container ---
	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("testuser"),
		tcpostgres.WithPassword("testpass"),
	)
	if err != nil {
		log.Fatalf("start postgres container: %v", err)
	}
	defer pgContainer.Terminate(ctx) //nolint:errcheck

	pgHost, err := pgContainer.Host(ctx)
	if err != nil {
		log.Fatalf("postgres host: %v", err)
	}
	pgPort, err := pgContainer.MappedPort(ctx, "5432/tcp")
	if err != nil {
		log.Fatalf("postgres port: %v", err)
	}

	// --- Redis container ---
	redisContainer, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		log.Fatalf("start redis container: %v", err)
	}
	defer redisContainer.Terminate(ctx) //nolint:errcheck

	redisHost, err := redisContainer.Host(ctx)
	if err != nil {
		log.Fatalf("redis host: %v", err)
	}
	redisPort, err := redisContainer.MappedPort(ctx, "6379/tcp")
	if err != nil {
		log.Fatalf("redis port: %v", err)
	}

	// --- Переменные окружения для конфига ---
	os.Setenv("GATEWAY_SIGN", testGatewaySign)
	os.Setenv("JWT_SECRET", "e2e-test-jwt-secret")
	os.Setenv("DB_USERS_HOST", pgHost)
	os.Setenv("DB_USERS_PORT", pgPort.Port())
	os.Setenv("DB_USERS_DATABASE", "testdb")
	os.Setenv("DB_USERS_USERNAME", "testuser")
	os.Setenv("DB_USERS_PASSWORD", "testpass")
	os.Setenv("SUBSCRIPTION_EXPIRY_WINDOWS", "7d,3d,1d")
	os.Setenv("EXPIRY_CHECK_INTERVAL", "1h")
	os.Setenv("USERS_PORT", "0")
	os.Setenv("LISTEN_BIND_IP", "127.0.0.1")
	os.Setenv("GATEWAY_HOST", "localhost")
	os.Setenv("GATEWAY_PORT", "8080")

	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// --- Postgres client ---
	db, err := postgresql.NewClient(ctx, 3, *cfg)
	if err != nil {
		log.Fatalf("pg client: %v", err)
	}
	defer db.Close()

	// --- Миграции ---
	if err := runMigrations(ctx, db); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	// --- Redis client для blocklist ---
	rdb := redis.NewClient(&redis.Options{
		Addr: redisHost + ":" + redisPort.Port(),
		DB:   1,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis ping: %v", err)
	}
	defer rdb.Close()

	// --- Репозитории ---
	pgRepo := postgresRepo.NewRepository(db)
	blockListRepo := blocklist.NewRepository(rdb)

	// --- Заглушки внешних сервисов ---
	notifClient := &noopNotificationClient{}
	emailSnd := &noopEmailSender{}

	// --- Сервис ---
	hub := wsHub.NewHub()
	svc := userService.NewService(pgRepo, &noopCassandraRepo{}, notifClient, hub, emailSnd, blockListRepo)

	// --- Обработчик ---
	handler := usersHandler.NewHandler(svc)

	// --- Роутер ---
	router := httprouter.New()
	handler.Register(router)

	// --- HTTP test server ---
	testServer = httptest.NewServer(router)
	defer testServer.Close()

	// --- Предзаполнение: тестовые пользователи с подписками ---
	seedTestUsers(ctx, db)

	os.Exit(m.Run())
}

// seedTestUsers создаёт предопределённых тестовых пользователей с активными подписками.
func seedTestUsers(ctx context.Context, db *pgxpool.Pool) {
	for _, u := range []struct {
		id    string
		name  string
		email string
	}{
		{testUserID, "Alice Test", "alice@test.com"},
		{testUserID2, "Bob Test", "bob@test.com"},
	} {
		db.Exec(ctx, //nolint:errcheck
			`INSERT INTO users (user_id, name, email, password)
			 VALUES ($1, $2, $3, '$2a$12$e2e_placeholder_not_for_login')
			 ON CONFLICT DO NOTHING`,
			u.id, u.name, u.email,
		)
		db.Exec(ctx, //nolint:errcheck
			`INSERT INTO user_roles (user_id, role_id) VALUES ($1, 1) ON CONFLICT DO NOTHING`,
			u.id,
		)
		db.Exec(ctx, //nolint:errcheck
			`INSERT INTO user_subscriptions (user_id, has_subscription, expires_at)
			 VALUES ($1, true, NOW() + INTERVAL '1 year')
			 ON CONFLICT (user_id) DO NOTHING`,
			u.id,
		)
	}
}

// runMigrations применяет все *.up.sql миграции в алфавитном порядке.
func runMigrations(ctx context.Context, db *pgxpool.Pool) error {
	migrationsDir := filepath.Join("..", "..", "migrations")
	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(files)

	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		if _, err := db.Exec(ctx, string(content)); err != nil {
			return fmt.Errorf("exec %s: %w", f, err)
		}
	}
	return nil
}
