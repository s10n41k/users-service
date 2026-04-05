package domain

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

const (
	MaxUsernameLen = 100
	MinPasswordLen = 6
	MaxPasswordLen = 72 // bcrypt limit
)

var (
	ErrEmptyEmail       = errors.New("email cannot be empty")
	ErrInvalidEmail     = errors.New("invalid email format")
	ErrEmptyName        = errors.New("name cannot be empty")
	ErrNameTooLong      = errors.New("name too long")
	ErrPasswordTooShort = errors.New("password too short")
	ErrPasswordTooLong  = errors.New("password too long")
)

// User — доменная сущность пользователя без зависимостей на инфраструктуру.
type User struct {
	ID              string
	Email           string
	Name            string
	PasswordHash    string
	Role            string
	HasSubscription bool
	ExpiresAt       *time.Time
	TelegramChatID  int64
	IsOAuth         bool
	IsBlocked       bool
	CreatedAt       time.Time
}

// ValidateEmail проверяет базовый формат email.
func ValidateEmail(email string) error {
	if strings.TrimSpace(email) == "" {
		return ErrEmptyEmail
	}
	at := strings.Index(email, "@")
	if at < 1 || at >= len(email)-1 {
		return ErrInvalidEmail
	}
	dot := strings.LastIndex(email, ".")
	if dot <= at+1 || dot >= len(email)-1 {
		return ErrInvalidEmail
	}
	return nil
}

// ValidateName проверяет имя пользователя.
func ValidateName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ErrEmptyName
	}
	if len(trimmed) > MaxUsernameLen {
		return ErrNameTooLong
	}
	return nil
}

// ValidatePassword проверяет минимальные требования к паролю.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLen {
		return ErrPasswordTooShort
	}
	if len(password) > MaxPasswordLen {
		return ErrPasswordTooLong
	}
	return nil
}

// IsPasswordComplex проверяет наличие цифр, строчных и заглавных букв.
func IsPasswordComplex(password string) bool {
	var hasUpper, hasLower, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	return hasUpper && hasLower && hasDigit
}

// IsSubscriptionActive возвращает true если подписка активна и не истекла.
func (u *User) IsSubscriptionActive() bool {
	if !u.HasSubscription {
		return false
	}
	if u.ExpiresAt == nil {
		return true
	}
	return u.ExpiresAt.After(time.Now())
}

// CanLogin возвращает false для заблокированных пользователей.
func (u *User) CanLogin() bool {
	return !u.IsBlocked
}
