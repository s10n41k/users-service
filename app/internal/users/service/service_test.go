package service

import (
	"TODOLIST/app/internal/users/errs"
	"TODOLIST/app/internal/users/model"
	"TODOLIST/app/internal/users/port/mocks"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// newService создаёт сервис с моками для unit-тестов.
func newService(t *testing.T) (
	*Service,
	*mocks.PostgresRepository,
	*mocks.CassandraRepository,
	*mocks.BlocklistRepository,
	*mocks.NotificationClient,
	*mocks.EmailSender,
) {
	t.Helper()
	pgRepo := &mocks.PostgresRepository{}
	cassRepo := &mocks.CassandraRepository{}
	blockList := &mocks.BlocklistRepository{}
	notifClient := &mocks.NotificationClient{}
	emailSender := &mocks.EmailSender{}

	svc := NewService(pgRepo, cassRepo, notifClient, nil, emailSender, blockList)
	return svc, pgRepo, cassRepo, blockList, notifClient, emailSender
}

// mustHashPassword хеширует пароль для использования в тестах.
func mustHashPassword(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return string(hash)
}

// --- CreateUser ---

func TestCreateUser_Success(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	pgRepo.On("CreateUser", mock.Anything, mock.MatchedBy(func(u model.UserTODO) bool {
		// пароль должен быть захеширован (bcrypt hash длиннее оригинала)
		return u.Email == "test@example.com" && u.Name == "Test User" && len(u.Password) > 20
	})).Return("user-uuid-1", nil)

	id, err := svc.CreateUser(context.Background(), model.UserTODO{
		Email:    "test@example.com",
		Name:     "Test User",
		Password: "plaintext123",
	})
	require.NoError(t, err)
	assert.Equal(t, "user-uuid-1", id)
	pgRepo.AssertExpectations(t)
}

func TestCreateUser_AlreadyExists(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	pgRepo.On("CreateUser", mock.Anything, mock.Anything).Return("", errs.ErrUserAlreadyExists)

	_, err := svc.CreateUser(context.Background(), model.UserTODO{
		Email: "exists@example.com", Name: "User", Password: "secret",
	})
	assert.ErrorIs(t, err, errs.ErrUserAlreadyExists)
}

func TestCreateUser_PasswordIsHashed(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	const plaintext = "plaintext123"
	pgRepo.On("CreateUser", mock.Anything, mock.MatchedBy(func(u model.UserTODO) bool {
		return u.Password != plaintext
	})).Return("id-1", nil)

	_, err := svc.CreateUser(context.Background(), model.UserTODO{
		Email: "u@example.com", Name: "U", Password: plaintext,
	})
	require.NoError(t, err)
}

// --- ValidateCredentials ---

func TestValidateCredentials_Success(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	hashed := mustHashPassword(t, "mypassword")
	user := &model.UserTODO{
		Id:       "user-1",
		Email:    "user@example.com",
		Name:     "User",
		Password: hashed,
		Role:     "user",
	}
	pgRepo.On("FindByEmail", mock.Anything, "user@example.com").Return(user, nil)

	resp, err := svc.ValidateCredentials(context.Background(), "user@example.com", "mypassword")
	require.NoError(t, err)
	assert.True(t, resp.Valid)
	assert.Equal(t, "user-1", resp.User.Id)
}

func TestValidateCredentials_UserNotFound(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	pgRepo.On("FindByEmail", mock.Anything, "nobody@example.com").Return(nil, nil)

	_, err := svc.ValidateCredentials(context.Background(), "nobody@example.com", "pass")
	assert.ErrorIs(t, err, errs.ErrUserNotFound)
}

func TestValidateCredentials_WrongPassword(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	hashed := mustHashPassword(t, "correctpassword")
	user := &model.UserTODO{
		Id:       "user-1",
		Email:    "user@example.com",
		Password: hashed,
	}
	pgRepo.On("FindByEmail", mock.Anything, "user@example.com").Return(user, nil)

	_, err := svc.ValidateCredentials(context.Background(), "user@example.com", "wrongpassword")
	assert.ErrorIs(t, err, errs.ErrMissingData)
}

func TestValidateCredentials_BlockedUser(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	hashed := mustHashPassword(t, "pass123")
	user := &model.UserTODO{
		Id:        "user-1",
		Email:     "blocked@example.com",
		Password:  hashed,
		IsBlocked: true,
	}
	pgRepo.On("FindByEmail", mock.Anything, "blocked@example.com").Return(user, nil)

	_, err := svc.ValidateCredentials(context.Background(), "blocked@example.com", "pass123")
	assert.ErrorIs(t, err, errs.ErrUserBlocked)
}

// --- BlockUser / UnblockUser ---

func TestBlockUser_Success(t *testing.T) {
	svc, pgRepo, _, blockList, _, _ := newService(t)

	pgRepo.On("BlockUser", mock.Anything, "user-1").Return(nil)
	blockList.On("Block", mock.Anything, "user-1").Return(nil)

	err := svc.BlockUser(context.Background(), "user-1")
	require.NoError(t, err)
	pgRepo.AssertExpectations(t)
	blockList.AssertExpectations(t)
}

func TestBlockUser_DBError(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	pgRepo.On("BlockUser", mock.Anything, "user-1").Return(errors.New("db error"))

	err := svc.BlockUser(context.Background(), "user-1")
	assert.Error(t, err)
}

func TestUnblockUser_Success(t *testing.T) {
	svc, pgRepo, _, blockList, _, _ := newService(t)

	pgRepo.On("UnblockUser", mock.Anything, "user-1").Return(nil)
	blockList.On("Unblock", mock.Anything, "user-1").Return(nil)

	err := svc.UnblockUser(context.Background(), "user-1")
	require.NoError(t, err)
}

// --- FindOneUser ---

func TestFindOneUser_Success(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	pgRepo.On("UserExists", mock.Anything, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").Return(true, nil)
	pgRepo.On("FindOneUser", mock.Anything, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").Return(model.UserTODO{
		Id:    "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		Email: "u@example.com",
		Name:  "User",
	}, nil)

	user, err := svc.FindOneUser(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	require.NoError(t, err)
	assert.Equal(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", user.Id)
}

func TestFindOneUser_NotFound(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	pgRepo.On("UserExists", mock.Anything, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaab").Return(false, nil)

	_, err := svc.FindOneUser(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaab")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- CreateOAuthUser ---

func TestCreateOAuthUser_Success(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	pgRepo.On("CreateUser", mock.Anything, mock.MatchedBy(func(u model.UserTODO) bool {
		return u.Email == "oauth@example.com" && u.IsOAuth
	})).Return("oauth-user-1", nil)

	id, err := svc.CreateOAuthUser(context.Background(), "oauth@example.com", "OAuth User")
	require.NoError(t, err)
	assert.Equal(t, "oauth-user-1", id)
}

func TestCreateOAuthUser_AlreadyExists(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	pgRepo.On("CreateUser", mock.Anything, mock.Anything).Return("", errs.ErrUserAlreadyExists)

	_, err := svc.CreateOAuthUser(context.Background(), "exists@example.com", "User")
	assert.ErrorIs(t, err, errs.ErrUserAlreadyExists)
}

// --- SendFriendRequest ---

func TestSendFriendRequest_RequesterNoSubscription(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	requester := model.UserTODO{
		Id:              "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		HasSubscription: false,
	}
	pgRepo.On("FindOneUser", mock.Anything, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").Return(requester, nil)

	err := svc.SendFriendRequest(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "подписке")
}

func TestSendFriendRequest_AddresseeNoSubscription(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	requester := model.UserTODO{Id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", HasSubscription: true}
	addressee := model.UserTODO{Id: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", HasSubscription: false}

	pgRepo.On("FindOneUser", mock.Anything, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").Return(requester, nil)
	pgRepo.On("FindOneUser", mock.Anything, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb").Return(addressee, nil)

	err := svc.SendFriendRequest(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "подписки")
}

func TestSendFriendRequest_Success(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	requester := model.UserTODO{
		Id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Name: "Alice", HasSubscription: true,
	}
	addressee := model.UserTODO{
		Id: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Name: "Bob", HasSubscription: true,
	}

	pgRepo.On("FindOneUser", mock.Anything, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").Return(requester, nil)
	pgRepo.On("FindOneUser", mock.Anything, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb").Return(addressee, nil)
	pgRepo.On("SendFriendRequest", mock.Anything, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb").Return(nil)

	err := svc.SendFriendRequest(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	require.NoError(t, err)
	pgRepo.AssertExpectations(t)
}

// --- ForgotPassword ---

func TestForgotPassword_UserNotFound(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	pgRepo.On("FindByEmail", mock.Anything, "noone@example.com").Return(nil, nil)

	err := svc.ForgotPassword(context.Background(), "noone@example.com")
	assert.Error(t, err)
}

func TestForgotPassword_SavesCode(t *testing.T) {
	svc, pgRepo, _, _, _, emailSender := newService(t)

	user := &model.UserTODO{Id: "u1", Email: "user@example.com"}
	pgRepo.On("FindByEmail", mock.Anything, "user@example.com").Return(user, nil)
	pgRepo.On("SavePasswordResetCode",
		mock.Anything,
		"user@example.com",
		mock.AnythingOfType("string"),
		mock.AnythingOfType("time.Time"),
	).Return(nil)
	// email отправляется в горутине — не ждём
	emailSender.On("SendPasswordResetCode", mock.Anything, mock.Anything).Return(nil).Maybe()

	err := svc.ForgotPassword(context.Background(), "user@example.com")
	require.NoError(t, err)
	pgRepo.AssertExpectations(t)
}

// --- ResetPassword ---

func TestResetPassword_WrongCode(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	user := &model.UserTODO{Id: "u1", Email: "user@example.com"}
	pgRepo.On("FindByEmail", mock.Anything, "user@example.com").Return(user, nil)
	pgRepo.On("GetPasswordResetCode", mock.Anything, "user@example.com").
		Return("1234", time.Now().Add(10*time.Minute), nil)

	err := svc.ResetPassword(context.Background(), "user@example.com", "9999", "newpassword")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "неверный код")
}

func TestResetPassword_ExpiredCode(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	user := &model.UserTODO{Id: "u1", Email: "user@example.com"}
	pgRepo.On("FindByEmail", mock.Anything, "user@example.com").Return(user, nil)
	pgRepo.On("GetPasswordResetCode", mock.Anything, "user@example.com").
		Return("1234", time.Now().Add(-1*time.Minute), nil)

	err := svc.ResetPassword(context.Background(), "user@example.com", "1234", "newpassword")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "истёк")
}

func TestResetPassword_Success(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	user := &model.UserTODO{Id: "u1", Email: "user@example.com"}
	pgRepo.On("FindByEmail", mock.Anything, "user@example.com").Return(user, nil)
	pgRepo.On("GetPasswordResetCode", mock.Anything, "user@example.com").
		Return("1234", time.Now().Add(10*time.Minute), nil)
	pgRepo.On("UpdateUser", mock.Anything, "u1", mock.MatchedBy(func(dto model.UserUpdateDTO) bool {
		return dto.Password != nil && len(*dto.Password) > 20
	})).Return("update ok", nil)
	pgRepo.On("DeletePasswordResetCode", mock.Anything, "user@example.com").Return(nil)

	err := svc.ResetPassword(context.Background(), "user@example.com", "1234", "newpassword")
	require.NoError(t, err)
	pgRepo.AssertExpectations(t)
}

// --- Exists (проверка email) ---

// Семантика: UserExistsEmail возвращает true если email СВОБОДЕН (count==0).
// Exists возвращает ErrUserAlreadyExists если email ЗАНЯТ (count>0, т.е. UserExistsEmail вернул false).

func TestExists_EmailFree(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	// true = email свободен (пользователя нет)
	pgRepo.On("UserExistsEmail", mock.Anything, "free@example.com").Return(true, nil)

	// Email свободен — Exists возвращает nil
	err := svc.Exists(context.Background(), "free@example.com")
	assert.NoError(t, err)
}

func TestExists_EmailTaken(t *testing.T) {
	svc, pgRepo, _, _, _, _ := newService(t)

	// false = email занят (пользователь существует)
	pgRepo.On("UserExistsEmail", mock.Anything, "taken@example.com").Return(false, nil)

	// Email занят — Exists возвращает ErrUserAlreadyExists
	err := svc.Exists(context.Background(), "taken@example.com")
	assert.ErrorIs(t, err, errs.ErrUserAlreadyExists)
}

// --- UpdateSubscription ---

func TestUpdateSubscription_FirstPurchase(t *testing.T) {
	svc, pgRepo, _, _, notifClient, _ := newService(t)

	// Пользователь без подписки (первая покупка)
	user := model.UserTODO{
		Id:              "user-1",
		Name:            "User",
		HasSubscription: false,
		ExpiresAt:       nil,
	}
	pgRepo.On("FindOneUser", mock.Anything, "user-1").Return(user, nil)
	pgRepo.On("UpdateSubscription", mock.Anything, "user-1", mock.AnythingOfType("time.Time")).Return(nil)
	pgRepo.On("DeleteNotificationRecords", mock.Anything, "user-1").Return(nil)
	pgRepo.On("RecordSubscriptionEvent", mock.Anything, "user-1", "purchased").Return(nil)
	notifClient.On("SyncSubscription", mock.Anything, mock.Anything).Return(nil)

	err := svc.UpdateSubscription(context.Background(), "user-1")
	require.NoError(t, err)
	pgRepo.AssertCalled(t, "RecordSubscriptionEvent", mock.Anything, "user-1", "purchased")
}

func TestUpdateSubscription_Renewal(t *testing.T) {
	svc, pgRepo, _, _, notifClient, _ := newService(t)

	future := time.Now().Add(10 * 24 * time.Hour)
	user := model.UserTODO{
		Id:              "user-1",
		Name:            "User",
		HasSubscription: true,
		ExpiresAt:       &future,
	}
	pgRepo.On("FindOneUser", mock.Anything, "user-1").Return(user, nil)
	pgRepo.On("UpdateSubscription", mock.Anything, "user-1", mock.AnythingOfType("time.Time")).Return(nil)
	pgRepo.On("DeleteNotificationRecords", mock.Anything, "user-1").Return(nil)
	pgRepo.On("RecordSubscriptionEvent", mock.Anything, "user-1", "renewed").Return(nil)
	notifClient.On("SyncSubscription", mock.Anything, mock.Anything).Return(nil)

	err := svc.UpdateSubscription(context.Background(), "user-1")
	require.NoError(t, err)
	pgRepo.AssertCalled(t, "RecordSubscriptionEvent", mock.Anything, "user-1", "renewed")
}
