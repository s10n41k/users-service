package main

import (
	"TODOLIST/app/internal/config"
	"TODOLIST/app/internal/users"
	"TODOLIST/app/internal/users/blocklist"
	cassandraRepo "TODOLIST/app/internal/users/db/cassandra"
	postgresql2 "TODOLIST/app/internal/users/db/postgresql"
	userSender "TODOLIST/app/internal/users/sender"
	service2 "TODOLIST/app/internal/users/service"
	ws "TODOLIST/app/internal/users/ws"
	notification "TODOLIST/app/notify"
	"TODOLIST/app/pkg/client/postgresql"
	"TODOLIST/app/pkg/metrics"
	"TODOLIST/app/worker"
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/julienschmidt/httprouter"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"
)

func main() {
	router := httprouter.New()
	ctx := context.Background()

	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatal(err)
	}

	clientPostgresUsers, err := postgresql.NewClient(ctx, 3, *cfg)
	if err != nil {
		log.Fatal(err)
	}
	if clientPostgresUsers == nil {
		log.Fatal("postgresql.NewClient() returned nil")
	}

	repositoryPostgresUsers := postgresql2.NewRepository(clientPostgresUsers)

	// Cassandra: подключаемся к кластеру для хранения сообщений
	repoCassandra, err := cassandraRepo.NewRepository(
		[]string{cfg.Cassandra.Host},
		cfg.Cassandra.Keyspace,
	)
	if err != nil {
		log.Fatalf("cassandra connect: %v", err)
	}

	// Redis blocklist (redis-auth) — для мгновенной блокировки пользователей
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Host + ":" + cfg.Redis.Port,
		Password: cfg.Redis.Password,
		DB:       1, // DB 1 — blocklist, чтобы не мешать сессиям auth-service (DB 0)
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("WARN: redis blocklist unavailable: %v (блокировка будет работать только через WS)", err)
		redisClient = nil
	}

	var blockListRepo blocklist.Repository
	if redisClient != nil {
		blockListRepo = blocklist.NewRepository(redisClient)
	}

	client := notification.NewHTTPNotificationClient(cfg.Gateway.Host, cfg.Gateway.Port, cfg.GatewaySign)

	wsHub := ws.NewHub()

	emailSender := userSender.NewEmailSender(*cfg)
	userService := service2.NewService(repositoryPostgresUsers, repoCassandra, client, wsHub, emailSender, blockListRepo)

	userHandler := users.NewHandler(userService)

	w := worker.NewSubscriptionExpiryWorker(repositoryPostgresUsers, client, cfg.SubscriptionExpiryWindows, cfg.ExpiryCheckInterval)

	go w.Start(ctx)

	userHandler.Register(router)

	metricsMW, metricsHandler := metrics.NewMiddleware("users_service")
	router.Handler("GET", "/metrics", metricsHandler)

	start(router, cfg, metricsMW)
}

func start(router *httprouter.Router, cfg *config.Config, metricsMW func(http.Handler) http.Handler) {
	log.Println("start application")

	var listener net.Listener
	var listenErr error

	if cfg.Listen.Type == "sock" {
		log.Println("detect app path")
		appDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			log.Fatal(err)
		}
		log.Println("create socket")
		socketPath := path.Join(appDir, "app.sock")

		log.Println("listen unix socket")
		listener, listenErr = net.Listen("unix", socketPath)
		log.Printf("server is listening unix socket: %s", socketPath)
	} else {
		log.Println("listen tcp")
		listener, listenErr = net.Listen("tcp", fmt.Sprintf("%s:%s", cfg.Listen.BindIP, cfg.Listen.Port))
		log.Printf("server is listening port %s:%s", cfg.Listen.BindIP, cfg.Listen.Port)
	}

	if listenErr != nil {
		log.Fatal(listenErr)
	}

	server := &http.Server{
		Handler:      metricsMW(router),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Fatal(server.Serve(listener))
}
