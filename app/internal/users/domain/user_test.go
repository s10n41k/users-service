package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr error
	}{
		{"valid", "user@example.com", nil},
		{"valid subdomain", "user@mail.example.com", nil},
		{"empty", "", ErrEmptyEmail},
		{"spaces only", "   ", ErrEmptyEmail},
		{"no at sign", "userexample.com", ErrInvalidEmail},
		{"at first", "@example.com", ErrInvalidEmail},
		{"no domain", "user@", ErrInvalidEmail},
		{"no dot in domain", "user@domain", ErrInvalidEmail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmail(tt.email)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"valid", "John Doe", nil},
		{"single char", "A", nil},
		{"empty", "", ErrEmptyName},
		{"spaces only", "   ", ErrEmptyName},
		{"max length", strings.Repeat("a", MaxUsernameLen), nil},
		{"over max length", strings.Repeat("a", MaxUsernameLen+1), ErrNameTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		pass    string
		wantErr error
	}{
		{"valid", "secret123", nil},
		{"exact min", strings.Repeat("a", MinPasswordLen), nil},
		{"too short", strings.Repeat("a", MinPasswordLen-1), ErrPasswordTooShort},
		{"max length", strings.Repeat("a", MaxPasswordLen), nil},
		{"over max length", strings.Repeat("a", MaxPasswordLen+1), ErrPasswordTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.pass)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUser_IsSubscriptionActive(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)

	tests := []struct {
		name string
		user User
		want bool
	}{
		{
			"active subscription with future expiry",
			User{HasSubscription: true, ExpiresAt: &future},
			true,
		},
		{
			"expired subscription",
			User{HasSubscription: true, ExpiresAt: &past},
			false,
		},
		{
			"no subscription",
			User{HasSubscription: false},
			false,
		},
		{
			"subscription without expiry date",
			User{HasSubscription: true, ExpiresAt: nil},
			true,
		},
		{
			"blocked user with active subscription",
			User{HasSubscription: true, ExpiresAt: &future, IsBlocked: true},
			true, // IsSubscriptionActive не проверяет блокировку
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.user.IsSubscriptionActive())
		})
	}
}

func TestUser_CanLogin(t *testing.T) {
	assert.True(t, (&User{IsBlocked: false}).CanLogin())
	assert.False(t, (&User{IsBlocked: true}).CanLogin())
}

func TestIsPasswordComplex(t *testing.T) {
	tests := []struct {
		name string
		pass string
		want bool
	}{
		{"all conditions met", "SecurePass1", true},
		{"no upper", "lowercase1", false},
		{"no lower", "UPPERCASE1", false},
		{"no digit", "NoDigitsHere", false},
		{"only digits", "12345678", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsPasswordComplex(tt.pass))
		})
	}
}
