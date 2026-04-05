package mocks

import (
	"TODOLIST/app/internal/users/model"
	notification "TODOLIST/app/notify"
	"context"
	"time"

	"github.com/stretchr/testify/mock"
)

// PostgresRepository — мок PostgresRepository.
type PostgresRepository struct{ mock.Mock }

func (m *PostgresRepository) UserExists(ctx context.Context, id string) (bool, error) {
	args := m.Called(ctx, id)
	return args.Bool(0), args.Error(1)
}

func (m *PostgresRepository) CreateUser(ctx context.Context, user model.UserTODO) (string, error) {
	args := m.Called(ctx, user)
	return args.String(0), args.Error(1)
}

func (m *PostgresRepository) FindAllUser(ctx context.Context) ([]model.UserFindAllDTO, error) {
	args := m.Called(ctx)
	return args.Get(0).([]model.UserFindAllDTO), args.Error(1)
}

func (m *PostgresRepository) FindOneUser(ctx context.Context, id string) (model.UserTODO, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(model.UserTODO), args.Error(1)
}

func (m *PostgresRepository) UpdateUser(ctx context.Context, id string, user model.UserUpdateDTO) (string, error) {
	args := m.Called(ctx, id, user)
	return args.String(0), args.Error(1)
}

func (m *PostgresRepository) DeleteUser(ctx context.Context, userID string) (string, error) {
	args := m.Called(ctx, userID)
	return args.String(0), args.Error(1)
}

func (m *PostgresRepository) FindByEmail(ctx context.Context, email string) (*model.UserTODO, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.UserTODO), args.Error(1)
}

func (m *PostgresRepository) UserExistsEmail(ctx context.Context, email string) (bool, error) {
	args := m.Called(ctx, email)
	return args.Bool(0), args.Error(1)
}

func (m *PostgresRepository) UpdateSubscription(ctx context.Context, userID string, expiresAt time.Time) error {
	return m.Called(ctx, userID, expiresAt).Error(0)
}

func (m *PostgresRepository) UpdateTelegramChatID(ctx context.Context, userID string, chatID int64) error {
	return m.Called(ctx, userID, chatID).Error(0)
}

func (m *PostgresRepository) FindUsersWithExpiringSubscription(ctx context.Context, windowKey string) ([]model.UserTODO, error) {
	args := m.Called(ctx, windowKey)
	return args.Get(0).([]model.UserTODO), args.Error(1)
}

func (m *PostgresRepository) InsertNotificationRecord(ctx context.Context, userID string, windowKey string) error {
	return m.Called(ctx, userID, windowKey).Error(0)
}

func (m *PostgresRepository) DeleteNotificationRecords(ctx context.Context, userID string) error {
	return m.Called(ctx, userID).Error(0)
}

func (m *PostgresRepository) SavePasswordResetCode(ctx context.Context, email, code string, expiresAt time.Time) error {
	return m.Called(ctx, email, code, expiresAt).Error(0)
}

func (m *PostgresRepository) GetPasswordResetCode(ctx context.Context, email string) (string, time.Time, error) {
	args := m.Called(ctx, email)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

func (m *PostgresRepository) DeletePasswordResetCode(ctx context.Context, email string) error {
	return m.Called(ctx, email).Error(0)
}

func (m *PostgresRepository) BlockUser(ctx context.Context, userID string) error {
	return m.Called(ctx, userID).Error(0)
}

func (m *PostgresRepository) UnblockUser(ctx context.Context, userID string) error {
	return m.Called(ctx, userID).Error(0)
}

func (m *PostgresRepository) SendFriendRequest(ctx context.Context, requesterID, addresseeID string) error {
	return m.Called(ctx, requesterID, addresseeID).Error(0)
}

func (m *PostgresRepository) AcceptFriendRequest(ctx context.Context, addresseeID, requesterID string) error {
	return m.Called(ctx, addresseeID, requesterID).Error(0)
}

func (m *PostgresRepository) RejectFriendRequest(ctx context.Context, addresseeID, requesterID string) error {
	return m.Called(ctx, addresseeID, requesterID).Error(0)
}

func (m *PostgresRepository) RemoveFriend(ctx context.Context, userID, friendID string) error {
	return m.Called(ctx, userID, friendID).Error(0)
}

func (m *PostgresRepository) GetFriends(ctx context.Context, userID string) ([]model.FriendInfo, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]model.FriendInfo), args.Error(1)
}

func (m *PostgresRepository) GetFriendRequests(ctx context.Context, userID string) ([]model.FriendRequest, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]model.FriendRequest), args.Error(1)
}

func (m *PostgresRepository) AreFriends(ctx context.Context, userID1, userID2 string) (bool, error) {
	args := m.Called(ctx, userID1, userID2)
	return args.Bool(0), args.Error(1)
}

func (m *PostgresRepository) RecordSubscriptionEvent(ctx context.Context, userID, eventType string) error {
	return m.Called(ctx, userID, eventType).Error(0)
}

func (m *PostgresRepository) GetUserStats(ctx context.Context, from, to string) (map[string]int, error) {
	args := m.Called(ctx, from, to)
	return args.Get(0).(map[string]int), args.Error(1)
}

func (m *PostgresRepository) CountRegisteredUsers(ctx context.Context, from, to string) (int, error) {
	args := m.Called(ctx, from, to)
	return args.Int(0), args.Error(1)
}

// CassandraRepository — мок CassandraRepository.
type CassandraRepository struct{ mock.Mock }

func (m *CassandraRepository) CreateMessage(ctx context.Context, msg model.Message) error {
	return m.Called(ctx, msg).Error(0)
}

func (m *CassandraRepository) GetMessages(ctx context.Context, userID, friendID, before string, limit int) ([]model.Message, error) {
	args := m.Called(ctx, userID, friendID, before, limit)
	return args.Get(0).([]model.Message), args.Error(1)
}

func (m *CassandraRepository) MarkMessagesRead(ctx context.Context, toUserID, fromUserID string) error {
	return m.Called(ctx, toUserID, fromUserID).Error(0)
}

func (m *CassandraRepository) GetUnreadCount(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *CassandraRepository) EditMessage(ctx context.Context, messageID, userID, newContent string) (*model.Message, error) {
	args := m.Called(ctx, messageID, userID, newContent)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Message), args.Error(1)
}

func (m *CassandraRepository) DeleteMessage(ctx context.Context, messageID, userID string) (string, string, error) {
	args := m.Called(ctx, messageID, userID)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *CassandraRepository) GetUnreadCountsPerFriend(ctx context.Context, userID string) (map[string]int, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).(map[string]int), args.Error(1)
}

func (m *CassandraRepository) SaveMessageHistory(ctx context.Context, messageID, content, eventType string) error {
	return m.Called(ctx, messageID, content, eventType).Error(0)
}

func (m *CassandraRepository) GetMessageHistory(ctx context.Context, messageID string) ([]model.MessageHistoryEntry, error) {
	args := m.Called(ctx, messageID)
	return args.Get(0).([]model.MessageHistoryEntry), args.Error(1)
}

// BlocklistRepository — мок blocklist.Repository.
type BlocklistRepository struct{ mock.Mock }

func (m *BlocklistRepository) Block(ctx context.Context, userID string) error {
	return m.Called(ctx, userID).Error(0)
}

func (m *BlocklistRepository) Unblock(ctx context.Context, userID string) error {
	return m.Called(ctx, userID).Error(0)
}

// NotificationClient — мок notification.Client.
type NotificationClient struct{ mock.Mock }

func (m *NotificationClient) SendExpiryNotification(ctx context.Context, user model.UserTODO, window string) error {
	return m.Called(ctx, user, window).Error(0)
}

func (m *NotificationClient) SyncSubscription(ctx context.Context, req notification.SubscriptionSyncRequest) error {
	return m.Called(ctx, req).Error(0)
}

func (m *NotificationClient) SendFriendRequestNotification(senderID string, chatID int64, senderName string) {
	m.Called(senderID, chatID, senderName)
}

// EmailSender — мок sender.EmailSender.
type EmailSender struct{ mock.Mock }

func (m *EmailSender) SendPasswordResetCode(email, code string) error {
	return m.Called(email, code).Error(0)
}
