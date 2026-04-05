package notification

import (
	"TODOLIST/app/internal/users/model"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SubscriptionSyncRequest — тело запроса синхронизации подписки с tasks-service.
type SubscriptionSyncRequest struct {
	UserID          string     `json:"user_id"`
	Name            string     `json:"name,omitempty"`
	HasSubscription bool       `json:"has_subscription"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	TelegramChatID  *int64     `json:"telegram_chat_id,omitempty"`
}

type Client interface {
	SendExpiryNotification(ctx context.Context, user model.UserTODO, window string) error
	SyncSubscription(ctx context.Context, req SubscriptionSyncRequest) error
	// SendFriendRequestNotification уведомляет пользователя о входящем запросе дружбы.
	// Вызывается в отдельной горутине (fire-and-forget).
	SendFriendRequestNotification(senderID string, chatID int64, senderName string)
}

type httpNotificationClient struct {
	gatewayURL   string
	notifySecret string
	httpClient   *http.Client
}

func NewHTTPNotificationClient(gatewayHost, gatewayPort, notifySecret string) Client {
	return &httpNotificationClient{
		gatewayURL:   fmt.Sprintf("http://%s:%s", gatewayHost, gatewayPort),
		notifySecret: notifySecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *httpNotificationClient) SyncSubscription(ctx context.Context, req SubscriptionSyncRequest) error {
	jsonBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal sync request: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(c.notifySecret))
	mac.Write(jsonBody)
	signature := hex.EncodeToString(mac.Sum(nil))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gatewayURL+"/internal/subscriptions/sync", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create sync request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Notify-Signature", signature)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send sync request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sync request failed with status: %d", resp.StatusCode)
	}
	return nil
}

func (c *httpNotificationClient) SendFriendRequestNotification(senderID string, chatID int64, senderName string) {
	body := map[string]interface{}{
		"type":             "friend_request",
		"telegram_chat_id": chatID,
		"user_id":          senderID,
		"sender_name":      senderName,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return
	}

	mac := hmac.New(sha256.New, []byte(c.notifySecret))
	mac.Write(jsonBody)
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodPost, c.gatewayURL+"/internal/notify", bytes.NewReader(jsonBody))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Notify-Signature", signature)

	// fire-and-forget: ошибка игнорируется намеренно —
	// неудачное Telegram-уведомление не должно мешать основному флоу
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func (c *httpNotificationClient) SendExpiryNotification(ctx context.Context, user model.UserTODO, window string) error {
	body := map[string]interface{}{
		"user_id":          user.Id,
		"telegram_chat_id": user.TelegramChatID,
		"type":             "subscription_expiry",
		"window":           window,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal notification body: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(c.notifySecret))
	mac.Write(jsonBody)
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gatewayURL+"/internal/notify", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Notify-Signature", signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("notification failed with status: %d", resp.StatusCode)
	}

	return nil
}
