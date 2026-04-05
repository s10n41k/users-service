package model

import "time"

type UserTODO struct {
	Id              string     `json:"id"`
	Email           string     `json:"email"`
	Name            string     `json:"name"`
	Password        string     `json:"password"`
	CreatedAt       time.Time  `json:"created_at"`
	Role            string     `json:"role"`
	HasSubscription bool       `json:"has_subscription"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	TelegramChatID  int64      `json:"telegram_chat_id,omitempty"`
	IsOAuth         bool       `json:"is_oauth"`
	IsBlocked       bool       `json:"is_blocked"`
}

// FriendInfo — данные друга для списка друзей в UI
type FriendInfo struct {
	UserID          string `json:"id"`
	Name            string `json:"name"`
	Email           string `json:"email"`
	HasSubscription bool   `json:"has_subscription"`
}

// FriendRequest — входящий запрос в друзья (данные отправителя)
type FriendRequest struct {
	RequesterID string    `json:"requester_id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	CreatedAt   time.Time `json:"created_at"`
}

// Message — одно сообщение в чате между двумя пользователями
type Message struct {
	MessageID   string     `json:"message_id"`
	FromUserID  string     `json:"from_user_id"`
	ToUserID    string     `json:"to_user_id"`
	Content     string     `json:"content"`
	CreatedAt   time.Time  `json:"created_at"`
	IsRead      bool       `json:"is_read"`
	IsSystem    bool       `json:"is_system"`
	EditedAt    *time.Time `json:"edited_at,omitempty"`
}

// ChangePasswordDTO — тело PATCH /users/:uuid/password
type ChangePasswordDTO struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ForgotPasswordRequest — тело POST /user/forgot-password
type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

// ResetPasswordRequest — тело POST /user/reset-password
type ResetPasswordRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

// MessageHistoryEntry — одна запись в истории версий сообщения
type MessageHistoryEntry struct {
	MessageID string    `json:"message_id"`
	VersionAt time.Time `json:"version_at"`
	Content   string    `json:"content"`
	EventType string    `json:"event_type"` // created, edited, deleted
}

// SendMessageDTO — тело POST /users/:id/messages/:fid
type SendMessageDTO struct {
	Content string `json:"content"`
}

// EditMessageDTO — тело PATCH /users/:uuid/messages/:mid
type EditMessageDTO struct {
	Content string `json:"content"`
}

// FriendActionDTO — тело POST /users/:id/friends/request
type FriendActionDTO struct {
	AddresseeID string `json:"addressee_id"`
}

