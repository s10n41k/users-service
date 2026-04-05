package postgresql

import (
	"TODOLIST/app/internal/users/errs"
	"TODOLIST/app/internal/users/model"
	"TODOLIST/app/internal/users/storagePostgres"
	"TODOLIST/app/pkg/client/postgresql"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"strings"
	"time"
)

const roleUser = 1
const roleAdmin = 2

type repository struct {
	ClientPostgres1 postgresql.Client
}

func (r *repository) UserExists(ctx context.Context, id string) (bool, error) {
	// Проверка на корректность формата UUID
	if _, err := uuid.Parse(id); err != nil {
		return false, fmt.Errorf("invalid uuid: %s", id)
	}

	// Запрос для проверки существования задачи
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE user_id = $1)`
	var exists bool
	err := r.ClientPostgres1.QueryRow(ctx, query, id).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if task exists: %w", err)
	}

	return exists, nil
}

func (r *repository) CreateUser(ctx context.Context, user model.UserTODO) (string, error) {
	tx, err := r.ClientPostgres1.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// 1. Создаём пользователя
	var userID string
	err = tx.QueryRow(ctx,
		"INSERT INTO users (name, email, password, is_oauth) VALUES ($1, $2, $3, $4) RETURNING user_id",
		user.Name, user.Email, user.Password, user.IsOAuth,
	).Scan(&userID)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return "", errs.ErrUserAlreadyExists
			}
		}
		return "", err
	}

	// 2. Присваиваем роль
	_, err = tx.Exec(ctx,
		"INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)",
		userID, roleUser,
	)
	if err != nil {
		return "", err
	}

	// 3. Коммитим транзакцию
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}

	return userID, nil
}

func (r *repository) FindAllUser(ctx context.Context) (users []model.UserFindAllDTO, err error) {
	rows, err := r.ClientPostgres1.Query(ctx, `
		SELECT
			u.user_id,
			u.name,
			u.email,
			u.created_at,
			COALESCE(ro.name, 'user') AS role,
			COALESCE(us.has_subscription, false) AS has_subscription,
			COALESCE(u.is_blocked, false) AS is_blocked
		FROM users u
		LEFT JOIN user_roles ur ON ur.user_id = u.user_id
		LEFT JOIN roles ro ON ro.role_id = ur.role_id
		LEFT JOIN user_subscriptions us ON us.user_id = u.user_id
		ORDER BY u.created_at DESC
	`)
	if err != nil {
		return users, err
	}
	defer rows.Close()
	for rows.Next() {
		var u model.UserFindAllDTO
		err = rows.Scan(&u.Id, &u.Name, &u.Email, &u.CreatedAt, &u.Role, &u.HasSubscription, &u.IsBlocked)
		if err != nil {
			return users, err
		}
		u.CreatedAt = u.CreatedAt.In(time.Local)
		users = append(users, u)
	}
	return users, nil
}

func (r *repository) BlockUser(ctx context.Context, userID string) error {
	_, err := r.ClientPostgres1.Exec(ctx, `UPDATE users SET is_blocked = TRUE WHERE user_id = $1`, userID)
	return err
}

func (r *repository) UnblockUser(ctx context.Context, userID string) error {
	_, err := r.ClientPostgres1.Exec(ctx, `UPDATE users SET is_blocked = FALSE WHERE user_id = $1`, userID)
	return err
}

func (r *repository) FindOneUser(ctx context.Context, id string) (user model.UserTODO, err error) {
	// Проверяем, является ли id UUID
	if _, err = uuid.Parse(id); err != nil {
		return user, fmt.Errorf("invalid user id")
	}

	query := `
        SELECT 
            u.user_id,
            u.name,
            u.email,
            u.created_at,
            COALESCE(r.name, 'user') AS role,
            us.has_subscription,
            us.expires_at,
            us.telegram_chat_id
        FROM users u
        LEFT JOIN user_subscriptions us ON us.user_id = u.user_id
        LEFT JOIN user_roles ur ON ur.user_id = u.user_id
        LEFT JOIN roles r ON r.role_id = ur.role_id
        WHERE u.user_id = $1
    `

	var (
		hasSub     sql.NullBool
		expiresAt  sql.NullTime
		telegramID sql.NullInt64
	)

	err = r.ClientPostgres1.QueryRow(ctx, query, id).
		Scan(&user.Id, &user.Name, &user.Email, &user.CreatedAt, &user.Role, &hasSub, &expiresAt, &telegramID)

	if err != nil {
		return user, err
	}

	user.CreatedAt = user.CreatedAt.In(time.Local)
	if hasSub.Valid {
		// Подписка активна только если has_subscription=true И expires_at ещё не истёк
		if expiresAt.Valid {
			user.HasSubscription = hasSub.Bool && expiresAt.Time.After(time.Now())
		} else {
			user.HasSubscription = hasSub.Bool
		}
	}

	if expiresAt.Valid {
		user.ExpiresAt = &expiresAt.Time
	}

	if telegramID.Valid {
		user.TelegramChatID = telegramID.Int64
	}
	return user, nil
}

func (r *repository) UpdateUser(ctx context.Context, id string, user model.UserUpdateDTO) (string, error) {
	// Проверяем, является ли id действительным UUID
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("invalid UUID: %s", id)
	}

	// Начинаем формирование запроса
	query := `UPDATE users SET `
	var values []interface{}
	var setClauses []string
	valueIndex := 1

	// Проверяем, какие поля переданы и формируем запрос
	if user.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", valueIndex))
		values = append(values, *user.Name)
		valueIndex++
	}

	if user.Email != nil {
		setClauses = append(setClauses, fmt.Sprintf("email = $%d", valueIndex))
		values = append(values, *user.Email)
		valueIndex++
	}

	if user.Password != nil {
		setClauses = append(setClauses, fmt.Sprintf("password = $%d", valueIndex))
		values = append(values, *user.Password)
		valueIndex++
	}

	// Соединяем части запроса
	query += fmt.Sprintf("%s WHERE user_id = $%d", strings.Join(setClauses, ", "), valueIndex)
	values = append(values, id)

	// Выполняем запрос на обновление с передачей новых значений
	_, err := r.ClientPostgres1.Exec(ctx, query, values...)
	if err != nil {
		return "", fmt.Errorf("failed to update user: %w", err)
	}

	return "update ok", nil
}

func (r *repository) DeleteUser(ctx context.Context, userId string) (string, error) {
	// Проверка на валидность UUID
	if _, err := uuid.Parse(userId); err != nil {
		return "invalid uuid", err
	}

	// Удаляем shared_subtasks где пользователь является исполнителем (нет ON DELETE CASCADE в миграции 0005)
	_, err := r.ClientPostgres1.Exec(ctx, `
		DELETE FROM shared_subtasks
		WHERE shared_task_id IN (
			SELECT id FROM shared_tasks WHERE proposer_id=$1 OR addressee_id=$1
		) OR assignee_id=$1`, userId)
	if err != nil {
		return "failed to delete shared_subtasks", err
	}

	_, err = r.ClientPostgres1.Exec(ctx, "DELETE FROM users WHERE user_id = $1", userId)
	if err != nil {
		return "failed to delete", err
	}
	return "delete ok", nil
}

func (r *repository) FindByEmail(ctx context.Context, email string) (*model.UserTODO, error) {
	query := `
        SELECT
            u.user_id,
            u.name,
            u.email,
            u.password,
            COALESCE(r.name, 'user') AS role,
            us.has_subscription,
            us.expires_at,
            us.telegram_chat_id,
            u.is_oauth,
            COALESCE(u.is_blocked, false) AS is_blocked
        FROM users u
        LEFT JOIN user_roles ur ON ur.user_id = u.user_id
        LEFT JOIN roles r ON r.role_id = ur.role_id
        LEFT JOIN user_subscriptions us ON us.user_id = u.user_id
        WHERE u.email = $1
    `

	var user model.UserTODO
	var (
		pasHash    []byte
		hasSub     sql.NullBool
		expiresAt  sql.NullTime
		telegramID sql.NullInt64
	)

	err := r.ClientPostgres1.QueryRow(ctx, query, email).Scan(
		&user.Id,
		&user.Name,
		&user.Email,
		&pasHash,
		&user.Role,
		&hasSub,
		&expiresAt,
		&telegramID,
		&user.IsOAuth,
		&user.IsBlocked,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find user by email: %w", err)
	}

	user.Password = string(pasHash)

	if hasSub.Valid {
		if expiresAt.Valid {
			user.HasSubscription = hasSub.Bool && expiresAt.Time.After(time.Now())
		} else {
			user.HasSubscription = hasSub.Bool
		}
	}

	if expiresAt.Valid {
		user.ExpiresAt = &expiresAt.Time
	}

	if telegramID.Valid {
		user.TelegramChatID = telegramID.Int64
	}

	return &user, nil
}

func (r *repository) UserExistsEmail(ctx context.Context, email string) (bool, error) {
	query := `SELECT COUNT(*) FROM users WHERE email = $1`

	var count int
	err := r.ClientPostgres1.QueryRow(ctx, query, email).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check email availability: %w", err)
	}

	// true если email свободен (count == 0)
	return count == 0, nil
}

func (r *repository) UpdateSubscription(ctx context.Context, userID string, expiresAt time.Time) error {
	query := `
        INSERT INTO user_subscriptions (user_id, has_subscription, subscribed_at, expires_at, updated_at)
        VALUES ($1, TRUE, NOW(), $2, NOW())
        ON CONFLICT (user_id) DO UPDATE SET
            has_subscription = TRUE,
            expires_at = $2,
            updated_at = NOW()`

	_, err := r.ClientPostgres1.Exec(ctx, query, userID, expiresAt)
	if err != nil {
		return fmt.Errorf("update subscription for user %s: %w", userID, err)
	}

	return nil
}

func (r *repository) UpdateTelegramChatID(ctx context.Context, userID string, chatID int64) error {
	query := `
	UPDATE user_subscriptions
	SET telegram_chat_id = $2,
    telegram_linked_at = NOW(),
    updated_at = NOW()
	WHERE user_id = $1`

	_, err := r.ClientPostgres1.Exec(ctx, query, userID, chatID)
	if err != nil {
		return fmt.Errorf("update telegram chat ID for user %s: %w", userID, err)
	}
	return nil
}

func (r *repository) FindUsersWithExpiringSubscription(ctx context.Context, windowKey string) ([]model.UserTODO, error) {
	query := `
        SELECT user_id, telegram_chat_id, expires_at
        FROM user_subscriptions
        WHERE expires_at <= NOW() + $1::INTERVAL
          AND expires_at > NOW()
          AND has_subscription = TRUE
          AND telegram_chat_id IS NOT NULL
          AND NOT EXISTS (
              SELECT 1 FROM subscription_notifications
              WHERE user_id = user_subscriptions.user_id
                AND window_key = $2
          )`

	rows, err := r.ClientPostgres1.Query(ctx, query, windowKey, windowKey)
	if err != nil {
		return nil, fmt.Errorf("find users with expiring subscription: %w", err)
	}
	defer rows.Close()

	var users []model.UserTODO
	for rows.Next() {
		var u model.UserTODO
		if err := rows.Scan(&u.Id, &u.TelegramChatID, &u.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan user subscription info: %w", err)
		}
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return users, nil
}

func (r *repository) InsertNotificationRecord(ctx context.Context, userID string, windowKey string) error {
	query := `INSERT INTO subscription_notifications (user_id, window_key, sent_at)
	VALUES ($1, $2, NOW())
	ON CONFLICT DO NOTHING`

	_, err := r.ClientPostgres1.Exec(ctx, query, userID, windowKey)
	if err != nil {
		return fmt.Errorf("insert notification record: %w", err)
	}
	return nil
}

func (r *repository) DeleteNotificationRecords(ctx context.Context, userID string) error {
	query := `DELETE FROM subscription_notifications WHERE user_id = $1
`
	_, err := r.ClientPostgres1.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("delete notification records: %w", err)
	}
	return nil
}

func NewRepository(client postgresql.Client) storagePostgres.PostgresRepository {
	return &repository{ClientPostgres1: client}
}

// ── Реализация методов сброса пароля ──────────────────────────────────────

func (r *repository) SavePasswordResetCode(ctx context.Context, email, code string, expiresAt time.Time) error {
	_, err := r.ClientPostgres1.Exec(ctx, `
		INSERT INTO password_reset_tokens (email, code, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE SET code = $2, expires_at = $3`,
		email, code, expiresAt)
	if err != nil {
		return fmt.Errorf("save password reset code: %w", err)
	}
	return nil
}

func (r *repository) GetPasswordResetCode(ctx context.Context, email string) (string, time.Time, error) {
	var code string
	var expiresAt time.Time
	err := r.ClientPostgres1.QueryRow(ctx, `
		SELECT code, expires_at FROM password_reset_tokens WHERE email = $1`, email).
		Scan(&code, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", time.Time{}, fmt.Errorf("код не найден")
		}
		return "", time.Time{}, fmt.Errorf("get password reset code: %w", err)
	}
	return code, expiresAt, nil
}

func (r *repository) DeletePasswordResetCode(ctx context.Context, email string) error {
	_, err := r.ClientPostgres1.Exec(ctx, `
		DELETE FROM password_reset_tokens WHERE email = $1`, email)
	if err != nil {
		return fmt.Errorf("delete password reset code: %w", err)
	}
	return nil
}

// ── Реализация методов дружбы ──────────────────────────────────────────────

func (r *repository) SendFriendRequest(ctx context.Context, requesterID, addresseeID string) error {
	// ON CONFLICT DO NOTHING — идемпотентно: повторный запрос не вызовет ошибку
	_, err := r.ClientPostgres1.Exec(ctx, `
		INSERT INTO friendships (requester_id, addressee_id, status)
		VALUES ($1::uuid, $2::uuid, 'pending')
		ON CONFLICT (requester_id, addressee_id) DO NOTHING`,
		requesterID, addresseeID)
	return err
}

func (r *repository) AcceptFriendRequest(ctx context.Context, addresseeID, requesterID string) error {
	// WHERE status='pending' — нельзя принять уже принятый/отклонённый запрос
	tag, err := r.ClientPostgres1.Exec(ctx, `
		UPDATE friendships SET status='accepted', updated_at=NOW()
		WHERE requester_id=$1::uuid AND addressee_id=$2::uuid AND status='pending'`,
		requesterID, addresseeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("запрос в друзья не найден")
	}
	return nil
}

func (r *repository) RejectFriendRequest(ctx context.Context, addresseeID, requesterID string) error {
	_, err := r.ClientPostgres1.Exec(ctx, `
		UPDATE friendships SET status='rejected', updated_at=NOW()
		WHERE requester_id=$1::uuid AND addressee_id=$2::uuid AND status='pending'`,
		requesterID, addresseeID)
	return err
}

func (r *repository) RemoveFriend(ctx context.Context, userID, friendID string) error {
	// Удаляем в обеих ориентациях — неизвестно кто был requester
	_, err := r.ClientPostgres1.Exec(ctx, `
		DELETE FROM friendships
		WHERE (requester_id=$1::uuid AND addressee_id=$2::uuid)
		   OR (requester_id=$2::uuid AND addressee_id=$1::uuid)`,
		userID, friendID)
	return err
}

func (r *repository) GetFriends(ctx context.Context, userID string) ([]model.FriendInfo, error) {
	// CASE WHEN определяет "кто из двух — это друг" (тот кто не является текущим userID)
	rows, err := r.ClientPostgres1.Query(ctx, `
		SELECT
			CASE WHEN f.requester_id=$1::uuid THEN f.addressee_id
			     ELSE f.requester_id END AS friend_id,
			u.name, u.email
		FROM friendships f
		JOIN users u ON u.user_id = CASE WHEN f.requester_id=$1::uuid
		    THEN f.addressee_id ELSE f.requester_id END
		WHERE (f.requester_id=$1::uuid OR f.addressee_id=$1::uuid)
		  AND f.status='accepted'
		ORDER BY u.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var friends []model.FriendInfo
	for rows.Next() {
		var fi model.FriendInfo
		if err := rows.Scan(&fi.UserID, &fi.Name, &fi.Email); err != nil {
			return nil, err
		}
		friends = append(friends, fi)
	}
	return friends, rows.Err()
}

func (r *repository) GetFriendRequests(ctx context.Context, userID string) ([]model.FriendRequest, error) {
	// Только WHERE addressee_id=$1 — входящие запросы
	rows, err := r.ClientPostgres1.Query(ctx, `
		SELECT f.requester_id, u.name, u.email, f.created_at
		FROM friendships f
		JOIN users u ON u.user_id = f.requester_id
		WHERE f.addressee_id=$1::uuid AND f.status='pending'
		ORDER BY f.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []model.FriendRequest
	for rows.Next() {
		var fr model.FriendRequest
		if err := rows.Scan(&fr.RequesterID, &fr.Name, &fr.Email, &fr.CreatedAt); err != nil {
			return nil, err
		}
		reqs = append(reqs, fr)
	}
	return reqs, rows.Err()
}

func (r *repository) AreFriends(ctx context.Context, userID1, userID2 string) (bool, error) {
	var count int
	err := r.ClientPostgres1.QueryRow(ctx, `
		SELECT COUNT(*) FROM friendships
		WHERE ((requester_id=$1::uuid AND addressee_id=$2::uuid)
		    OR (requester_id=$2::uuid AND addressee_id=$1::uuid))
		  AND status='accepted'`, userID1, userID2).Scan(&count)
	return count > 0, err
}

// ── Реализация событий подписки ────────────────────────────────────────────

func (r *repository) RecordSubscriptionEvent(ctx context.Context, userID, eventType string) error {
	_, err := r.ClientPostgres1.Exec(ctx, `
		INSERT INTO subscription_events (user_id, event_type, created_at)
		VALUES ($1::uuid, $2, NOW())`, userID, eventType)
	if err != nil {
		return fmt.Errorf("record subscription event: %w", err)
	}
	return nil
}

func (r *repository) GetUserStats(ctx context.Context, from, to string) (map[string]int, error) {
	query := `
		SELECT event_type, COUNT(*) as cnt
		FROM subscription_events
		WHERE ($1 = '' OR created_at >= $1::timestamptz)
		  AND ($2 = '' OR created_at <= $2::timestamptz)
		GROUP BY event_type`

	rows, err := r.ClientPostgres1.Query(ctx, query, from, to)
	if err != nil {
		return nil, fmt.Errorf("get user stats: %w", err)
	}
	defer rows.Close()

	result := map[string]int{
		"purchased": 0,
		"renewed":   0,
		"expired":   0,
	}
	for rows.Next() {
		var eventType string
		var cnt int
		if err := rows.Scan(&eventType, &cnt); err != nil {
			continue
		}
		result[eventType] = cnt
	}

	// Количество зарегистрированных пользователей за период
	registered, err := r.CountRegisteredUsers(ctx, from, to)
	if err == nil {
		result["registered"] = registered
	}

	return result, rows.Err()
}

func (r *repository) CountRegisteredUsers(ctx context.Context, from, to string) (int, error) {
	var count int
	err := r.ClientPostgres1.QueryRow(ctx, `
		SELECT COUNT(*) FROM users
		WHERE ($1 = '' OR created_at >= $1::timestamptz)
		  AND ($2 = '' OR created_at <= $2::timestamptz)`, from, to).Scan(&count)
	return count, err
}

