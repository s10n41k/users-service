package cassandra

import (
	"TODOLIST/app/internal/users/model"
	"TODOLIST/app/internal/users/storagePostgres"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

type cassandraDB struct {
	session *gocql.Session
}

// NewRepository создаёт CassandraRepository.
// hosts — список нод Cassandra (например []string{"cassandra"})
// keyspace — "todolist"
func NewRepository(hosts []string, keyspace string) (storagePostgres.CassandraRepository, error) {
	cluster := gocql.NewCluster(hosts...)
	cluster.Keyspace = keyspace
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = 5 * time.Second
	cluster.ConnectTimeout = 15 * time.Second
	cluster.NumConns = 2
	cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{NumRetries: 3}

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("cassandra connect: %w", err)
	}
	return &cassandraDB{session: session}, nil
}

func (db *cassandraDB) Close() {
	db.session.Close()
}

// conversationID возвращает стабильный ID диалога независимо от порядка участников.
// Берём меньший UUID первым — диалог A↔B всегда имеет одинаковый ключ.
func conversationID(userA, userB string) string {
	if userA < userB {
		return userA + "_" + userB
	}
	return userB + "_" + userA
}

// friendIDFor возвращает ID друга (тот кто не является currentUser).
func friendIDFor(currentUser, fromUser, toUser string) string {
	if currentUser == fromUser {
		return toUser
	}
	return fromUser
}

// CreateMessage сохраняет сообщение и обновляет связанные таблицы.
// Выполняет четыре операции в Cassandra:
//  1. INSERT в messages
//  2. INSERT в message_lookup (индекс для edit/delete по message_id)
//  3. UPDATE counter в unread_counts у получателя
//  4. INSERT/UPDATE в conversations для обоих участников
func (db *cassandraDB) CreateMessage(ctx context.Context, msg model.Message) error {
	convID := conversationID(msg.FromUserID, msg.ToUserID)
	ts := gocql.TimeUUID()

	msgUUID, err := gocql.ParseUUID(msg.MessageID)
	if err != nil {
		return fmt.Errorf("parse message_id: %w", err)
	}
	fromUUID, err := gocql.ParseUUID(msg.FromUserID)
	if err != nil {
		return fmt.Errorf("parse from_user_id: %w", err)
	}
	toUUID2, err := gocql.ParseUUID(msg.ToUserID)
	if err != nil {
		return fmt.Errorf("parse to_user_id for lookup: %w", err)
	}

	// Операция 1: Само сообщение
	if err := db.session.Query(`
		INSERT INTO messages
		    (conversation_id, created_at, message_id, from_user_id, content, is_read, is_system)
		VALUES (?, ?, ?, ?, ?, false, ?)`,
		convID, ts, msgUUID, fromUUID, msg.Content, msg.IsSystem,
	).WithContext(ctx).Exec(); err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	// Сохраняем оригинальный текст в историю (для панели администратора)
	if !msg.IsSystem {
		if err := db.SaveMessageHistory(ctx, msg.MessageID, msg.Content, "created"); err != nil {
			log.Printf("warn: save message history (created) %s: %v", msg.MessageID, err)
		}
	}

	// Операция 2: Lookup-запись для поиска по message_id (нужна для edit/delete)
	if err := db.session.Query(`
		INSERT INTO message_lookup
		    (message_id, conversation_id, created_at, from_user_id, to_user_id, is_system)
		VALUES (?, ?, ?, ?, ?, ?)`,
		msgUUID, convID, ts, fromUUID, toUUID2, msg.IsSystem,
	).WithContext(ctx).Exec(); err != nil {
		return fmt.Errorf("insert message_lookup: %w", err)
	}

	// Операция 3: Инкремент счётчика непрочитанных у получателя
	if err := db.session.Query(`
		UPDATE unread_counts SET count = count + 1
		WHERE user_id = ? AND conversation_id = ?`,
		toUUID2, convID,
	).WithContext(ctx).Exec(); err != nil {
		return fmt.Errorf("update unread counter: %w", err)
	}

	// Операция 3: Обновить превью диалога для обоих участников
	preview := msg.Content
	if len([]rune(preview)) > 60 {
		runes := []rune(preview)
		preview = string(runes[:60]) + "…"
	}

	for _, uid := range []string{msg.FromUserID, msg.ToUserID} {
		userUUID, err := gocql.ParseUUID(uid)
		if err != nil {
			continue
		}
		friendUUID, err := gocql.ParseUUID(friendIDFor(uid, msg.FromUserID, msg.ToUserID))
		if err != nil {
			continue
		}
		if err := db.session.Query(`
			INSERT INTO conversations
			    (user_id, last_message_at, conversation_id, friend_id, last_message)
			VALUES (?, ?, ?, ?, ?)`,
			userUUID, ts, convID, friendUUID, preview,
		).WithContext(ctx).Exec(); err != nil {
			return fmt.Errorf("update conversations for %s: %w", uid, err)
		}
	}

	return nil
}

// GetMessages возвращает страницу сообщений диалога.
// before — TIMEUUID последнего известного сообщения (cursor для пагинации).
// Если before пустой — возвращает самые свежие сообщения.
// Сообщения приходят от свежих к старым (ORDER BY created_at DESC).
func (db *cassandraDB) GetMessages(ctx context.Context, userID, friendID, before string, limit int) ([]model.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	convID := conversationID(userID, friendID)

	var iter *gocql.Iter

	if before == "" {
		// Первая загрузка: последние N сообщений
		iter = db.session.Query(`
			SELECT message_id, from_user_id, content, created_at, is_read, is_system, edited_at
			FROM messages
			WHERE conversation_id = ?
			LIMIT ?`,
			convID, limit,
		).WithContext(ctx).Iter()
	} else {
		// Cursor-based пагинация: сообщения старее before
		beforeUUID, err := gocql.ParseUUID(before)
		if err != nil {
			return nil, fmt.Errorf("неверный cursor: %w", err)
		}
		iter = db.session.Query(`
			SELECT message_id, from_user_id, content, created_at, is_read, is_system, edited_at
			FROM messages
			WHERE conversation_id = ?
			AND created_at < ?
			LIMIT ?`,
			convID, beforeUUID, limit,
		).WithContext(ctx).Iter()
	}

	var msgs []model.Message
	var (
		msgID    gocql.UUID
		fromUser gocql.UUID
		content  string
		ts       gocql.UUID
		isRead   bool
		isSystem bool
		editedAt time.Time
	)
	for iter.Scan(&msgID, &fromUser, &content, &ts, &isRead, &isSystem, &editedAt) {
		msg := model.Message{
			MessageID:  msgID.String(),
			FromUserID: fromUser.String(),
			ToUserID:   friendIDFor(fromUser.String(), userID, friendID),
			Content:    content,
			CreatedAt:  ts.Time(),
			IsRead:     isRead,
			IsSystem:   isSystem,
		}
		if !editedAt.IsZero() {
			t := editedAt
			msg.EditedAt = &t
		}
		msgs = append(msgs, msg)
	}
	return msgs, iter.Close()
}

// MarkMessagesRead помечает все непрочитанные входящие сообщения как прочитанные
// и обнуляет счётчик unread_counts.
func (db *cassandraDB) MarkMessagesRead(ctx context.Context, toUserID, fromUserID string) error {
	convID := conversationID(toUserID, fromUserID)

	// Шаг 1: Найти все непрочитанные сообщения в этом диалоге
	// ALLOW FILTERING оправдан: фильтруем только внутри одного partition (одного диалога)
	iter := db.session.Query(`
		SELECT created_at FROM messages
		WHERE conversation_id = ?
		AND is_read = false
		ALLOW FILTERING`,
		convID,
	).WithContext(ctx).Iter()

	var toUpdate []gocql.UUID
	var ts gocql.UUID
	for iter.Scan(&ts) {
		toUpdate = append(toUpdate, ts)
	}
	if err := iter.Close(); err != nil {
		return fmt.Errorf("scan unread messages: %w", err)
	}

	// Шаг 2: Пометить каждое сообщение как прочитанное
	for _, msgTS := range toUpdate {
		if err := db.session.Query(`
			UPDATE messages SET is_read = true
			WHERE conversation_id = ? AND created_at = ?`,
			convID, msgTS,
		).WithContext(ctx).Exec(); err != nil {
			return fmt.Errorf("mark read: %w", err)
		}
	}

	// Шаг 3: Обнулить счётчик (DELETE строки = счётчик станет 0 при следующем чтении)
	toUUID, err := gocql.ParseUUID(toUserID)
	if err != nil {
		return fmt.Errorf("parse to_user_id: %w", err)
	}
	return db.session.Query(`
		DELETE FROM unread_counts
		WHERE user_id = ? AND conversation_id = ?`,
		toUUID, convID,
	).WithContext(ctx).Exec()
}

// GetUnreadCountsPerFriend возвращает реальное количество непрочитанных сообщений
// по каждому другу, считая напрямую из таблицы messages.
// Это точнее чем unread_counts counter, который мог накопить неверные данные.
func (db *cassandraDB) GetUnreadCountsPerFriend(ctx context.Context, userID string) (map[string]int, error) {
	userUUID, err := gocql.ParseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user_id: %w", err)
	}

	// Шаг 1: получить все диалоги пользователя из таблицы conversations
	// (user_id — partition key, запрос без ALLOW FILTERING — эффективен)
	type convInfo struct {
		convID   string
		friendID gocql.UUID
	}
	var convs []convInfo

	convIter := db.session.Query(`
		SELECT conversation_id, friend_id FROM conversations
		WHERE user_id = ?`,
		userUUID,
	).WithContext(ctx).Iter()

	var cid string
	var fid gocql.UUID
	for convIter.Scan(&cid, &fid) {
		convs = append(convs, convInfo{convID: cid, friendID: fid})
	}
	if err := convIter.Close(); err != nil {
		return nil, fmt.Errorf("get conversations: %w", err)
	}

	result := map[string]int{}

	// Шаг 2: для каждого диалога считаем непрочитанные сообщения FROM друга TO нас.
	// ALLOW FILTERING внутри одного партишна (conversation_id = ?) — эффективен,
	// т.к. все сообщения одного диалога на одной ноде.
	for _, c := range convs {
		var count int
		if err := db.session.Query(`
			SELECT COUNT(*) FROM messages
			WHERE conversation_id = ? AND from_user_id = ? AND is_read = false
			ALLOW FILTERING`,
			c.convID, c.friendID,
		).WithContext(ctx).Scan(&count); err == nil && count > 0 {
			result[c.friendID.String()] = count
		}
	}

	return result, nil
}

// GetUnreadCount возвращает суммарное количество непрочитанных сообщений
// по всем диалогам пользователя.
func (db *cassandraDB) GetUnreadCount(ctx context.Context, userID string) (int, error) {
	userUUID, err := gocql.ParseUUID(userID)
	if err != nil {
		return 0, fmt.Errorf("parse user_id: %w", err)
	}

	iter := db.session.Query(`
		SELECT count FROM unread_counts
		WHERE user_id = ?`,
		userUUID,
	).WithContext(ctx).Iter()

	var total int64
	var count int64
	for iter.Scan(&count) {
		total += count
	}
	return int(total), iter.Close()
}

// msgLookup — результат поиска сообщения по message_id
type msgLookup struct {
	convID    string
	createdAt gocql.UUID
	fromUser  gocql.UUID
	toUser    gocql.UUID
	isSystem  bool
}

// lookupMessage ищет сообщение сначала в message_lookup (быстро),
// затем через вторичный индекс messages(message_id) как fallback
// для сообщений, отправленных до появления lookup-таблицы.
func (db *cassandraDB) lookupMessage(ctx context.Context, msgUUID gocql.UUID) (*msgLookup, error) {
	var res msgLookup

	// Шаг 1: message_lookup (O(1), основной путь)
	err := db.session.Query(`
		SELECT conversation_id, created_at, from_user_id, to_user_id, is_system
		FROM message_lookup WHERE message_id = ?`, msgUUID,
	).WithContext(ctx).Scan(&res.convID, &res.createdAt, &res.fromUser, &res.toUser, &res.isSystem)

	if err == nil {
		return &res, nil
	}
	if !errors.Is(err, gocql.ErrNotFound) {
		return nil, fmt.Errorf("lookup message: %w", err)
	}

	// Шаг 2: fallback через вторичный индекс (для старых сообщений без lookup-записи)
	iter := db.session.Query(`
		SELECT conversation_id, created_at, from_user_id, is_system
		FROM messages WHERE message_id = ?`, msgUUID,
	).WithContext(ctx).Iter()

	found := iter.Scan(&res.convID, &res.createdAt, &res.fromUser, &res.isSystem)
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("fallback lookup: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("сообщение не найдено")
	}

	// Вычисляем to_user_id из conversation_id (формат: "uuid1_uuid2")
	parts := strings.SplitN(res.convID, "_", 2)
	if len(parts) == 2 {
		fromStr := res.fromUser.String()
		if parts[0] == fromStr {
			res.toUser, _ = gocql.ParseUUID(parts[1])
		} else {
			res.toUser, _ = gocql.ParseUUID(parts[0])
		}
	}

	// Backfill: записываем в lookup чтобы следующий раз находилось быстро
	_ = db.session.Query(`
		INSERT INTO message_lookup
		    (message_id, conversation_id, created_at, from_user_id, to_user_id, is_system)
		VALUES (?, ?, ?, ?, ?, ?)`,
		msgUUID, res.convID, res.createdAt, res.fromUser, res.toUser, res.isSystem,
	).WithContext(ctx).Exec()

	return &res, nil
}

// EditMessage изменяет текст сообщения.
// Возвращает обновлённую структуру Message с fromUserID и toUserID для WS-уведомлений.
func (db *cassandraDB) EditMessage(ctx context.Context, messageID, userID, newContent string) (*model.Message, error) {
	msgUUID, err := gocql.ParseUUID(messageID)
	if err != nil {
		return nil, fmt.Errorf("неверный message_id: %w", err)
	}

	lu, err := db.lookupMessage(ctx, msgUUID)
	if err != nil {
		return nil, err
	}
	if lu.fromUser.String() != userID {
		return nil, fmt.Errorf("нельзя редактировать чужое сообщение")
	}
	if lu.isSystem {
		return nil, fmt.Errorf("системные сообщения нельзя редактировать")
	}

	editedAt := time.Now()
	if err := db.session.Query(`
		UPDATE messages SET content = ?, edited_at = ?
		WHERE conversation_id = ? AND created_at = ?`,
		newContent, editedAt, lu.convID, lu.createdAt,
	).WithContext(ctx).Exec(); err != nil {
		return nil, fmt.Errorf("update message: %w", err)
	}

	// Сохраняем новую версию в историю
	if err := db.SaveMessageHistory(ctx, messageID, newContent, "edited"); err != nil {
		log.Printf("warn: save message history (edited) %s: %v", messageID, err)
	}

	return &model.Message{
		MessageID:  messageID,
		FromUserID: lu.fromUser.String(),
		ToUserID:   lu.toUser.String(),
		Content:    newContent,
		EditedAt:   &editedAt,
	}, nil
}

// DeleteMessage удаляет сообщение из messages и message_lookup.
// Возвращает fromUserID, toUserID для отправки WS-уведомлений обоим участникам.
func (db *cassandraDB) DeleteMessage(ctx context.Context, messageID, userID string) (string, string, error) {
	msgUUID, err := gocql.ParseUUID(messageID)
	if err != nil {
		return "", "", fmt.Errorf("неверный message_id: %w", err)
	}

	lu, err := db.lookupMessage(ctx, msgUUID)
	if err != nil {
		return "", "", err
	}
	if lu.fromUser.String() != userID {
		return "", "", fmt.Errorf("нельзя удалить чужое сообщение")
	}
	if lu.isSystem {
		return "", "", fmt.Errorf("системные сообщения нельзя удалять")
	}

	// Сохраняем контент в историю перед удалением
	var lastContent string
	_ = db.session.Query(`
		SELECT content FROM messages WHERE conversation_id = ? AND created_at = ?`,
		lu.convID, lu.createdAt,
	).WithContext(ctx).Scan(&lastContent)
	if err := db.SaveMessageHistory(ctx, messageID, lastContent, "deleted"); err != nil {
		log.Printf("warn: save message history (deleted) %s: %v", messageID, err)
	}

	if err := db.session.Query(`
		DELETE FROM messages WHERE conversation_id = ? AND created_at = ?`,
		lu.convID, lu.createdAt,
	).WithContext(ctx).Exec(); err != nil {
		return "", "", fmt.Errorf("delete message: %w", err)
	}

	// Удаляем из lookup если запись там есть (для старых сообщений её может не быть)
	_ = db.session.Query(`
		DELETE FROM message_lookup WHERE message_id = ?`, msgUUID,
	).WithContext(ctx).Exec()

	return lu.fromUser.String(), lu.toUser.String(), nil
}

// SaveMessageHistory сохраняет версию сообщения в историю.
// event_type: "created" | "edited" | "deleted"
func (db *cassandraDB) SaveMessageHistory(ctx context.Context, messageID, content, eventType string) error {
	msgUUID, err := gocql.ParseUUID(messageID)
	if err != nil {
		return fmt.Errorf("parse message_id: %w", err)
	}
	return db.session.Query(`
		INSERT INTO message_history (message_id, version_at, content, event_type)
		VALUES (?, now(), ?, ?)`,
		msgUUID, content, eventType,
	).WithContext(ctx).Exec()
}

// GetMessageHistory возвращает все версии сообщения в хронологическом порядке.
func (db *cassandraDB) GetMessageHistory(ctx context.Context, messageID string) ([]model.MessageHistoryEntry, error) {
	msgUUID, err := gocql.ParseUUID(messageID)
	if err != nil {
		return nil, fmt.Errorf("parse message_id: %w", err)
	}
	iter := db.session.Query(`
		SELECT version_at, content, event_type
		FROM message_history WHERE message_id = ?`,
		msgUUID,
	).WithContext(ctx).Iter()

	var entries []model.MessageHistoryEntry
	var (
		versionAt gocql.UUID
		content   string
		eventType string
	)
	for iter.Scan(&versionAt, &content, &eventType) {
		entries = append(entries, model.MessageHistoryEntry{
			MessageID: messageID,
			VersionAt: versionAt.Time(),
			Content:   content,
			EventType: eventType,
		})
	}
	return entries, iter.Close()
}
