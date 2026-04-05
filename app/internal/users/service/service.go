package service

import (
	"TODOLIST/app/internal/users/blocklist"
	"TODOLIST/app/internal/users/errs"
	"TODOLIST/app/internal/users/model"
	"TODOLIST/app/internal/users/sender"
	"TODOLIST/app/internal/users/storagePostgres"
	wsModule "TODOLIST/app/internal/users/ws"
	notification "TODOLIST/app/notify"
	"context"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"log"
	"math/rand"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repositoryPostgres  storagePostgres.PostgresRepository
	repositoryCassandra storagePostgres.CassandraRepository
	syncClient          notification.Client
	Hub                 *wsModule.Hub
	emailSender         sender.EmailSender
	blockList           blocklist.Repository
}

func NewService(
	repositoryPostgres storagePostgres.PostgresRepository,
	repositoryCassandra storagePostgres.CassandraRepository,
	syncClient notification.Client,
	hub *wsModule.Hub,
	emailSender sender.EmailSender,
	blockList blocklist.Repository,
) *Service {
	return &Service{
		repositoryPostgres:  repositoryPostgres,
		repositoryCassandra: repositoryCassandra,
		syncClient:          syncClient,
		Hub:                 hub,
		emailSender:         emailSender,
		blockList:           blockList,
	}
}

// ValidateCredentials - бизнес-логика проверки учетных данных
// CreateOAuthUser создаёт пользователя через OAuth без пароля.
// В поле пароля сохраняется неугадываемая строка, исключающая обычный вход.
func (s *Service) CreateOAuthUser(ctx context.Context, email, name string) (string, error) {
	// Используем уникальный хеш вместо пароля — bcrypt не сможет его подобрать
	sentinelPassword := "$oauth$" + uuid.New().String()
	hashed, err := bcrypt.GenerateFromPassword([]byte(sentinelPassword), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to generate sentinel password: %w", err)
	}
	user := model.UserTODO{
		Email:    email,
		Name:     name,
		Password: string(hashed),
		IsOAuth:  true,
	}
	id, err := s.repositoryPostgres.CreateUser(ctx, user)
	if err != nil {
		if errors.Is(err, errs.ErrUserAlreadyExists) {
			return "", errs.ErrUserAlreadyExists
		}
		return "", err
	}
	return id, nil
}

func (s *Service) ValidateCredentials(ctx context.Context, email, password string) (*model.LoginResponse, error) {
	// Ищем пользователя по email
	user, err := s.repositoryPostgres.FindByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	if user == nil {
		return nil, errs.ErrUserNotFound
	}

	// Проверяем блокировку
	if user.IsBlocked {
		return nil, errs.ErrUserBlocked
	}

	// Проверяем пароль
	isValid := s.verifyPassword(password, user.Password)
	if !isValid {
		return nil, errs.ErrMissingData
	}
	return &model.LoginResponse{
		User: &model.UserTODO{
			Id:              user.Id,
			Email:           user.Email,
			Name:            user.Name,
			Password:        user.Password,
			CreatedAt:       user.CreatedAt,
			Role:            user.Role,
			HasSubscription: user.HasSubscription,
		},
		Valid: true,
	}, nil
}

func (s *Service) BlockUser(ctx context.Context, userID string) error {
	if err := s.repositoryPostgres.BlockUser(ctx, userID); err != nil {
		return err
	}
	// Добавляем в Redis blocklist — gateway откажет в любом запросе мгновенно
	if s.blockList != nil {
		if err := s.blockList.Block(ctx, userID); err != nil {
			log.Printf("BlockUser: redis blocklist write failed for %s: %v", userID, err)
		}
	}
	// WS: принудительный вылет заблокированного пользователя (если онлайн)
	if s.Hub != nil {
		s.Hub.Send(userID, wsModule.Event{
			Type: "force_logout",
			Data: map[string]string{"reason": "account_blocked"},
		})
	}
	return nil
}

func (s *Service) UnblockUser(ctx context.Context, userID string) error {
	if err := s.repositoryPostgres.UnblockUser(ctx, userID); err != nil {
		return err
	}
	// Убираем из Redis blocklist
	if s.blockList != nil {
		if err := s.blockList.Unblock(ctx, userID); err != nil {
			log.Printf("UnblockUser: redis blocklist delete failed for %s: %v", userID, err)
		}
	}
	return nil
}

func (s *Service) UpdateTelegramChatID(ctx context.Context, userID string, ChatID int64) error {
	err := s.repositoryPostgres.UpdateTelegramChatID(ctx, userID, ChatID)
	if err != nil {
		return err
	}

	// Синхронизируем telegram_chat_id с tasks-service
	user, fetchErr := s.repositoryPostgres.FindOneUser(ctx, userID)
	if fetchErr == nil {
		chatID := ChatID
		syncReq := notification.SubscriptionSyncRequest{
			UserID:          userID,
			Name:            user.Name,
			HasSubscription: user.HasSubscription,
			ExpiresAt:       user.ExpiresAt,
			TelegramChatID:  &chatID,
		}
		if syncErr := s.syncClient.SyncSubscription(ctx, syncReq); syncErr != nil {
			log.Printf("UpdateTelegramChatID: sync subscription user %s: %v", userID, syncErr)
		}
	}
	return nil
}

func (s *Service) UpdateSubscription(ctx context.Context, userID string) error {
	user, err := s.repositoryPostgres.FindOneUser(ctx, userID)
	if err != nil {
		return err
	}

	// Определяем тип события: продление или первая покупка
	eventType := "purchased"
	if user.ExpiresAt != nil && user.ExpiresAt.After(time.Now()) {
		eventType = "renewed"
	}

	var expiresAt time.Time
	if user.ExpiresAt != nil && user.ExpiresAt.After(time.Now()) {
		expiresAt = user.ExpiresAt.Add(30 * 24 * time.Hour)
	} else {
		expiresAt = time.Now().Add(30 * 24 * time.Hour)
	}

	err = s.repositoryPostgres.UpdateSubscription(ctx, userID, expiresAt)
	if err != nil {
		return err
	}
	err = s.repositoryPostgres.DeleteNotificationRecords(ctx, userID)
	if err != nil {
		return err
	}

	// Записываем событие подписки
	if recErr := s.repositoryPostgres.RecordSubscriptionEvent(ctx, userID, eventType); recErr != nil {
		log.Printf("UpdateSubscription: record event user %s: %v", userID, recErr)
	}

	// Синхронизируем подписку с tasks-service
	var chatID *int64
	if user.TelegramChatID != 0 {
		c := user.TelegramChatID
		chatID = &c
	}
	syncReq := notification.SubscriptionSyncRequest{
		UserID:          userID,
		Name:            user.Name,
		HasSubscription: true,
		ExpiresAt:       &expiresAt,
		TelegramChatID:  chatID,
	}
	if syncErr := s.syncClient.SyncSubscription(ctx, syncReq); syncErr != nil {
		log.Printf("UpdateSubscription: sync subscription user %s: %v", userID, syncErr)
	}
	return nil
}

func (s *Service) verifyPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
const bcryptCost = 12

func (s *Service) CreateUser(ctx context.Context, user model.UserTODO) (string, error) {
	hashPassword, errGen := bcrypt.GenerateFromPassword([]byte(user.Password), bcryptCost)
	if errGen != nil {
		return "", fmt.Errorf("failed to generate password: %w", errGen)
	}
	user.Password = string(hashPassword)

	id, err := s.repositoryPostgres.CreateUser(ctx, user)
	if err != nil {
		if errors.Is(err, errs.ErrUserAlreadyExists) {
			return "", errs.ErrUserAlreadyExists
		}
		return "", err
	}
	return id, nil
}

func (s *Service) FindAllUser(ctx context.Context) (users []model.UserFindAllDTO, err error) {
	users, err = s.repositoryPostgres.FindAllUser(ctx)
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (s *Service) FindOneUser(ctx context.Context, id string) (user model.UserTODO, err error) {
	ok, err := s.repositoryPostgres.UserExists(ctx, id)
	if err != nil {
		return user, err
	}
	if !ok {
		return user, errors.New("user not found")
	}
	user, err = s.repositoryPostgres.FindOneUser(ctx, id)
	if err != nil {
		return model.UserTODO{}, err
	}
	return user, nil
}

func (s *Service) UpdateUser(ctx context.Context, id string, user model.UserUpdateDTO) (string, error) {
	ok, err := s.repositoryPostgres.UserExists(ctx, id)
	if err != nil {
		return "", err
	}
	if !ok {
		return "not found", errors.New("user not found")
	}
	result, err := s.repositoryPostgres.UpdateUser(ctx, id, user)
	if err != nil {
		return "", err
	}
	// Синхронизируем новое имя с tasks-service
	if user.Name != nil && *user.Name != "" {
		if fetchedUser, fetchErr := s.repositoryPostgres.FindOneUser(ctx, id); fetchErr == nil {
			var chatID *int64
			if fetchedUser.TelegramChatID != 0 {
				c := fetchedUser.TelegramChatID
				chatID = &c
			}
			syncReq := notification.SubscriptionSyncRequest{
				UserID:          id,
				Name:            *user.Name,
				HasSubscription: fetchedUser.HasSubscription,
				ExpiresAt:       fetchedUser.ExpiresAt,
				TelegramChatID:  chatID,
			}
			if syncErr := s.syncClient.SyncSubscription(ctx, syncReq); syncErr != nil {
				log.Printf("UpdateUser: sync user %s: %v", id, syncErr)
			}
		}
	}
	return result, nil
}

func (s *Service) DeleteUser(ctx context.Context, id string) (string, error) {
	ok, err := s.repositoryPostgres.UserExists(ctx, id)
	if err != nil {
		return "", err
	}
	if !ok {
		return "not found", errors.New("user not found")
	}
	result, err := s.repositoryPostgres.DeleteUser(ctx, id)
	if err != nil {
		return "", err
	}
	return result, nil
}

func (s *Service) Exists(ctx context.Context, email string) error {
	ok, err := s.repositoryPostgres.UserExistsEmail(ctx, email)
	if err != nil {
		return err
	}
	if !ok {
		return errs.ErrUserAlreadyExists
	}
	return nil
}

// ── Методы дружбы ─────────────────────────────────────────────────────────

func (s *Service) SendFriendRequest(ctx context.Context, requesterID, addresseeID string) error {
	// Только подписчики могут добавлять друзей
	requester, err := s.repositoryPostgres.FindOneUser(ctx, requesterID)
	if err != nil {
		return err
	}
	if !requester.HasSubscription {
		return fmt.Errorf("функция друзей доступна только по подписке")
	}

	// Получатель запроса должен существовать
	addressee, err := s.repositoryPostgres.FindOneUser(ctx, addresseeID)
	if err != nil {
		return fmt.Errorf("пользователь не найден")
	}

	// Оба участника должны иметь PRO подписку
	if !addressee.HasSubscription {
		return fmt.Errorf("у этого пользователя нет PRO подписки")
	}

	if err := s.repositoryPostgres.SendFriendRequest(ctx, requesterID, addresseeID); err != nil {
		return err
	}

	// WS: уведомить адресата в реальном времени
	if s.Hub != nil {
		s.Hub.Send(addresseeID, wsModule.Event{
			Type: "friend_request",
			Data: map[string]string{
				"from_user_id": requesterID,
				"from_name":    requester.Name,
			},
		})
	}
	// Уведомить через Telegram (fire-and-forget, не блокирует ответ)
	if addressee.TelegramChatID != 0 {
		go s.syncClient.SendFriendRequestNotification(requesterID, addressee.TelegramChatID, requester.Name)
	}
	return nil
}

func (s *Service) AcceptFriendRequest(ctx context.Context, addresseeID, requesterID string) error {
	if err := s.repositoryPostgres.AcceptFriendRequest(ctx, addresseeID, requesterID); err != nil {
		return err
	}
	// WS: уведомить отправителя, что заявка принята
	if s.Hub != nil {
		s.Hub.Send(requesterID, wsModule.Event{
			Type: "friend_accepted",
			Data: map[string]string{"user_id": addresseeID},
		})
	}
	return nil
}

func (s *Service) RejectFriendRequest(ctx context.Context, addresseeID, requesterID string) error {
	return s.repositoryPostgres.RejectFriendRequest(ctx, addresseeID, requesterID)
}

func (s *Service) RemoveFriend(ctx context.Context, userID, friendID string) error {
	return s.repositoryPostgres.RemoveFriend(ctx, userID, friendID)
}

func (s *Service) GetFriends(ctx context.Context, userID string) ([]model.FriendInfo, error) {
	return s.repositoryPostgres.GetFriends(ctx, userID)
}

func (s *Service) GetFriendRequests(ctx context.Context, userID string) ([]model.FriendRequest, error) {
	return s.repositoryPostgres.GetFriendRequests(ctx, userID)
}

// ── Методы сообщений ──────────────────────────────────────────────────────

func (s *Service) SendMessage(ctx context.Context, fromUserID, toUserID, content string, isSystem bool) (string, error) {
	// Системные сообщения (от tasks-service) не требуют проверки дружбы
	if !isSystem {
		ok, err := s.repositoryPostgres.AreFriends(ctx, fromUserID, toUserID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("сообщения доступны только друзьям")
		}
	}

	msg := model.Message{
		MessageID:  uuid.New().String(),
		FromUserID: fromUserID,
		ToUserID:   toUserID,
		Content:    content,
		IsSystem:   isSystem,
	}
	if err := s.repositoryCassandra.CreateMessage(ctx, msg); err != nil {
		return "", err
	}
	// WS: уведомить получателя в реальном времени
	if s.Hub != nil {
		s.Hub.Send(toUserID, wsModule.Event{
			Type: "chat_message",
			Data: map[string]interface{}{
				"from_user_id": fromUserID,
				"content":      content,
				"message_id":   msg.MessageID,
				"is_system":    isSystem,
			},
		})
	}
	return msg.MessageID, nil
}

// EditMessage изменяет текст сообщения и рассылает WS-событие обоим участникам.
func (s *Service) EditMessage(ctx context.Context, messageID, userID, content string) (*model.Message, error) {
	msg, err := s.repositoryCassandra.EditMessage(ctx, messageID, userID, content)
	if err != nil {
		return nil, err
	}
	if s.Hub != nil {
		event := wsModule.Event{
			Type: "chat_message_edited",
			Data: map[string]interface{}{
				"message_id":   messageID,
				"from_user_id": msg.FromUserID,
				"content":      content,
				"edited_at":    msg.EditedAt,
			},
		}
		s.Hub.Send(msg.FromUserID, event)
		s.Hub.Send(msg.ToUserID, event)
	}
	return msg, nil
}

// DeleteMessage удаляет сообщение и рассылает WS-событие обоим участникам.
func (s *Service) DeleteMessage(ctx context.Context, messageID, userID string) error {
	fromUID, toUID, err := s.repositoryCassandra.DeleteMessage(ctx, messageID, userID)
	if err != nil {
		return err
	}
	if s.Hub != nil {
		event := wsModule.Event{
			Type: "chat_message_deleted",
			Data: map[string]interface{}{
				"message_id":   messageID,
				"from_user_id": fromUID,
			},
		}
		s.Hub.Send(fromUID, event)
		s.Hub.Send(toUID, event)
	}
	return nil
}

func (s *Service) GetMessages(ctx context.Context, userID, friendID, before string, limit int) ([]model.Message, error) {
	// Проверяем дружбу — нельзя читать чужой диалог
	ok, err := s.repositoryPostgres.AreFriends(ctx, userID, friendID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("нет доступа к этому диалогу")
	}
	return s.repositoryCassandra.GetMessages(ctx, userID, friendID, before, limit)
}

func (s *Service) MarkMessagesRead(ctx context.Context, toUserID, fromUserID string) error {
	return s.repositoryCassandra.MarkMessagesRead(ctx, toUserID, fromUserID)
}

func (s *Service) GetUnreadCount(ctx context.Context, userID string) (int, error) {
	return s.repositoryCassandra.GetUnreadCount(ctx, userID)
}

func (s *Service) GetUnreadCountsPerFriend(ctx context.Context, userID string) (map[string]int, error) {
	return s.repositoryCassandra.GetUnreadCountsPerFriend(ctx, userID)
}

// ── Смена пароля ──────────────────────────────────────────────────────────

// ChangePassword — проверяет старый пароль и устанавливает новый
func (s *Service) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	user, err := s.repositoryPostgres.FindOneUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("пользователь не найден")
	}
	// Получаем хеш пароля отдельным запросом (FindOneUser не возвращает password)
	fullUser, err := s.repositoryPostgres.FindByEmail(ctx, user.Email)
	if err != nil || fullUser == nil {
		return fmt.Errorf("не удалось получить данные пользователя")
	}
	if !s.verifyPassword(oldPassword, fullUser.Password) {
		return fmt.Errorf("неверный текущий пароль")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("ошибка хеширования пароля")
	}
	hashedStr := string(hashed)
	_, err = s.repositoryPostgres.UpdateUser(ctx, userID, model.UserUpdateDTO{Password: &hashedStr})
	return err
}

// ── Сброс пароля ──────────────────────────────────────────────────────────

// ForgotPassword — генерирует код и отправляет на email
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	user, err := s.repositoryPostgres.FindByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("ошибка поиска пользователя")
	}
	if user == nil {
		return fmt.Errorf("пользователь с таким email не найден")
	}
	code := fmt.Sprintf("%04d", rand.Intn(9000)+1000)
	expiresAt := time.Now().Add(10 * time.Minute)
	if err := s.repositoryPostgres.SavePasswordResetCode(ctx, email, code, expiresAt); err != nil {
		return fmt.Errorf("ошибка сохранения кода: %w", err)
	}
	go func() {
		if s.emailSender != nil {
			if err := s.emailSender.SendPasswordResetCode(email, code); err != nil {
				log.Printf("ForgotPassword: не удалось отправить письмо: %v", err)
			}
		}
	}()
	return nil
}

// ResetPassword — проверяет код и устанавливает новый пароль
func (s *Service) ResetPassword(ctx context.Context, email, code, newPassword string) error {
	user, err := s.repositoryPostgres.FindByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("ошибка поиска пользователя")
	}
	if user == nil {
		return fmt.Errorf("пользователь не найден")
	}
	savedCode, expiresAt, err := s.repositoryPostgres.GetPasswordResetCode(ctx, email)
	if err != nil {
		return fmt.Errorf("код не найден или уже использован")
	}
	if time.Now().After(expiresAt) {
		return fmt.Errorf("код истёк, запросите новый")
	}
	if savedCode != code {
		return fmt.Errorf("неверный код")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("ошибка хеширования пароля")
	}
	hashedStr := string(hashed)
	if _, err := s.repositoryPostgres.UpdateUser(ctx, user.Id, model.UserUpdateDTO{Password: &hashedStr}); err != nil {
		return fmt.Errorf("ошибка обновления пароля: %w", err)
	}
	_ = s.repositoryPostgres.DeletePasswordResetCode(ctx, email)
	return nil
}

// FindUserByEmailFull — полный объект пользователя по email (включая is_oauth).
func (s *Service) FindUserByEmailFull(ctx context.Context, email string) (*model.UserTODO, error) {
	return s.repositoryPostgres.FindByEmail(ctx, email)
}

// AdminGetChat — получение истории сообщений между двумя пользователями без проверки дружбы.
func (s *Service) AdminGetChat(ctx context.Context, userID, friendID, before string, limit int) ([]model.Message, error) {
	return s.repositoryCassandra.GetMessages(ctx, userID, friendID, before, limit)
}

// AdminGetMessageHistory — все версии сообщения для панели администратора.
func (s *Service) AdminGetMessageHistory(ctx context.Context, messageID string) ([]model.MessageHistoryEntry, error) {
	return s.repositoryCassandra.GetMessageHistory(ctx, messageID)
}

// GetUserStats — статистика событий подписки за период.
func (s *Service) GetUserStats(ctx context.Context, from, to string) (map[string]int, error) {
	return s.repositoryPostgres.GetUserStats(ctx, from, to)
}

// FindUserByEmail — поиск пользователя по email (для отправки заявки в друзья из UI)
func (s *Service) FindUserByEmail(ctx context.Context, email string) (*model.FriendInfo, error) {
	user, err := s.repositoryPostgres.FindByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}
	return &model.FriendInfo{
		UserID:          user.Id,
		Name:            user.Name,
		Email:           user.Email,
		HasSubscription: user.HasSubscription,
	}, nil
}
