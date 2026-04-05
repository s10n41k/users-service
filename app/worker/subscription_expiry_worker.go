package worker

import (
	"TODOLIST/app/internal/users/storagePostgres"
	notification "TODOLIST/app/notify"
	"context"
	"log"
	"log/slog"
	"sync"
	"time"
)

type SubscriptionExpiryWorker struct {
	repo          storagePostgres.PostgresRepository
	gatewayClient notification.Client
	windows       []string
	checkInterval time.Duration
}

func NewSubscriptionExpiryWorker(repo storagePostgres.PostgresRepository, client notification.Client, windows []string, checkInterval time.Duration) *SubscriptionExpiryWorker {
	return &SubscriptionExpiryWorker{
		repo:          repo,
		gatewayClient: client,
		windows:       windows,
		checkInterval: checkInterval,
	}
}

func (s *SubscriptionExpiryWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping subscription expiry worker")
			return
		case <-ticker.C:
			s.processAllWindows(ctx)
		}
	}
}

func (s *SubscriptionExpiryWorker) processAllWindows(ctx context.Context) {
	wg := sync.WaitGroup{}

	for _, window := range s.windows {
		wg.Add(1)
		go func(ctx context.Context, window string) {
			defer wg.Done()
			s.processWindow(ctx, window)
		}(ctx, window)
	}
	wg.Wait()
}

func (s *SubscriptionExpiryWorker) processWindow(ctx context.Context, window string) {
	users, err := s.repo.FindUsersWithExpiringSubscription(ctx, window)
	if err != nil {
		log.Printf("expiry worker: find users for window %s: %v", window, err)
	}

	for _, user := range users {
		err = s.gatewayClient.SendExpiryNotification(ctx, user, window)
		if err != nil {
			log.Printf("expiry worker: send expiry notification for window %s: %v", window, err)
		}
		err = s.repo.InsertNotificationRecord(ctx, user.Id, window)
		if err != nil {
			log.Printf("expiry worker: insert notification record for window %s: %v", window, err)
		}
	}

}
