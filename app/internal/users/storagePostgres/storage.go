package storagePostgres

import (
	"TODOLIST/app/internal/users/model"
	"context"
	"time"
)

type PostgresRepository interface {
	UserExists(ctx context.Context, id string) (bool, error)
	CreateUser(ctx context.Context, user model.UserTODO) (string, error)
	FindAllUser(ctx context.Context) (users []model.UserFindAllDTO, err error)
	FindOneUser(ctx context.Context, id string) (user model.UserTODO, err error)
	UpdateUser(ctx context.Context, id string, user model.UserUpdateDTO) (string, error)
	DeleteUser(ctx context.Context, userId string) (string, error)
	FindByEmail(ctx context.Context, email string) (*model.UserTODO, error)
	UserExistsEmail(ctx context.Context, email string) (bool, error)
	UpdateSubscription(ctx context.Context, userID string, expiresAt time.Time) error
	UpdateTelegramChatID(ctx context.Context, userID string, chatID int64) error
	FindUsersWithExpiringSubscription(ctx context.Context, windowKey string) ([]model.UserTODO, error)
	InsertNotificationRecord(ctx context.Context, userID string, windowKey string) error
	DeleteNotificationRecords(ctx context.Context, userID string) error

	// ── Сброс пароля ─────────────────────────────────────────────────────
	SavePasswordResetCode(ctx context.Context, email, code string, expiresAt time.Time) error
	GetPasswordResetCode(ctx context.Context, email string) (code string, expiresAt time.Time, err error)
	DeletePasswordResetCode(ctx context.Context, email string) error

	// ── Блокировка ───────────────────────────────────────────────────────
	BlockUser(ctx context.Context, userID string) error
	UnblockUser(ctx context.Context, userID string) error

	// ── Дружба ──────────────────────────────────────────────────────────
	SendFriendRequest(ctx context.Context, requesterID, addresseeID string) error
	AcceptFriendRequest(ctx context.Context, addresseeID, requesterID string) error
	RejectFriendRequest(ctx context.Context, addresseeID, requesterID string) error
	RemoveFriend(ctx context.Context, userID, friendID string) error
	GetFriends(ctx context.Context, userID string) ([]model.FriendInfo, error)
	GetFriendRequests(ctx context.Context, userID string) ([]model.FriendRequest, error)
	AreFriends(ctx context.Context, userID1, userID2 string) (bool, error)

	// ── Статистика подписок ──────────────────────────────────────────────
	RecordSubscriptionEvent(ctx context.Context, userID, eventType string) error
	GetUserStats(ctx context.Context, from, to string) (map[string]int, error)
	CountRegisteredUsers(ctx context.Context, from, to string) (int, error)
}

// CassandraRepository — контракт хранилища сообщений в Cassandra
type CassandraRepository interface {
	CreateMessage(ctx context.Context, msg model.Message) error
	GetMessages(ctx context.Context, userID, friendID, before string, limit int) ([]model.Message, error)
	MarkMessagesRead(ctx context.Context, toUserID, fromUserID string) error
	GetUnreadCount(ctx context.Context, userID string) (int, error)
	// EditMessage изменяет текст сообщения. Возвращает обновлённое сообщение.
	EditMessage(ctx context.Context, messageID, userID, newContent string) (*model.Message, error)
	// DeleteMessage удаляет сообщение. Возвращает fromUserID, toUserID для WS-уведомлений.
	DeleteMessage(ctx context.Context, messageID, userID string) (string, string, error)
	// GetUnreadCountsPerFriend возвращает счётчик непрочитанных по каждому другу отдельно.
	// Ключ — friend_id (UUID строкой), значение — количество сообщений.
	GetUnreadCountsPerFriend(ctx context.Context, userID string) (map[string]int, error)

	// SaveMessageHistory сохраняет версию сообщения в историю (для панели администратора).
	SaveMessageHistory(ctx context.Context, messageID, content, eventType string) error
	// GetMessageHistory возвращает все версии сообщения в хронологическом порядке.
	GetMessageHistory(ctx context.Context, messageID string) ([]model.MessageHistoryEntry, error)
}
