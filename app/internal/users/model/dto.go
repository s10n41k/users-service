package model

import (
	"time"
)

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=6"`
}

// RegisterRequest - запрос на регистрацию
type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Name     string `json:"name" validate:"required"`
	Password string `json:"password" validate:"required,min=6"`
}

// LoginResponse - ответ при успешной проверке credentials
type LoginResponse struct {
	User  *UserTODO `json:"user"`
	Valid bool      `json:"valid"`
}

type UserCreateDTO struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type UserUpdateDTO struct {
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Password *string `json:"password"`
}

type UserFindAllDTO struct {
	Id              string    `json:"id"`
	Email           string    `json:"email"`
	Name            string    `json:"name"`
	CreatedAt       time.Time `json:"created_at"`
	Role            string    `json:"role"`
	HasSubscription bool      `json:"has_subscription"`
	IsBlocked       bool      `json:"is_blocked"`
}

type UpdateSubscriptionDTO struct {
	ExpiresAt time.Time `json:"expires_at"`
}

type UpdateTelegramDTO struct {
	TelegramChatID int64 `json:"telegram_chat_id"`
}

// OAuthRegisterRequest — запрос на регистрацию пользователя через OAuth (без пароля).
type OAuthRegisterRequest struct {
	Email string `json:"email" validate:"required,email"`
	Name  string `json:"name"  validate:"required"`
}
