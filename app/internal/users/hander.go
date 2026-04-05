package users

import (
	"TODOLIST/app/internal/apperror"
	"TODOLIST/app/internal/handlers"
	"TODOLIST/app/internal/users/errs"
	"TODOLIST/app/internal/users/model"
	service2 "TODOLIST/app/internal/users/service"
	ws "TODOLIST/app/internal/users/ws"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	"log"
	"net/http"
)

const (
	usersURL              = "/users"
	userURL               = "/users/:uuid"
	registerURL           = "/user/register"
	loginURL              = "/user/login"
	oauthRegisterURL      = "/user/oauth-register"
	userExistsURL         = "/user/check-email/:email"
	userSubscriptionsURL  = "/users/:uuid/subscription"
	userTelegramChatIDURL = "/users/:uuid/chatID"

	// Дружба
	// POST /users/:uuid/friends — отправить заявку (тело: {addressee_id})
	// GET  /users/:uuid/friends — список друзей
	userFriendsURL      = "/users/:uuid/friends"
	userFriendInboxURL  = "/users/:uuid/friends-inbox" // GET — входящие заявки
	userFriendAcceptURL = "/users/:uuid/friends/:fid/accept"
	userFriendRejectURL = "/users/:uuid/friends/:fid/reject"
	userFriendDeleteURL = "/users/:uuid/friends/:fid"

	// Сообщения
	userMessagesURL        = "/users/:uuid/messages/:fid"
	userMessagesReadURL    = "/users/:uuid/messages/:fid/read"
	userUnreadCountURL     = "/users/:uuid/messages-unread"            // GET — суммарный счётчик
	userUnreadPerFriendURL = "/users/:uuid/messages-unread-per-friend" // GET — по каждому другу
	// Редактирование и удаление по message_id (параметр :fid — совместим с httprouter trie)
	userMessageActionURL = "/users/:uuid/messages/:fid" // PATCH/DELETE — по message_id

	// Поиск пользователя по email (для добавления в друзья)
	userFindByEmailURL = "/user/by-email/:email"

	// Смена пароля
	userChangePasswordURL = "/users/:uuid/password"

	// Сброс пароля (публичные, без JWT)
	userForgotPasswordURL = "/user/forgot-password"
	userResetPasswordURL  = "/user/reset-password"

	// WebSocket
	userWSURL = "/users/:uuid/ws"
)

type handler struct {
	service *service2.Service
}

func NewHandler(service *service2.Service) handlers.Handler {
	return &handler{service: service}
}

func (h *handler) Register(router *httprouter.Router) {

	errorMW := func(hf apperror.Handler) http.HandlerFunc {
		return apperror.Middleware(hf)
	}

	router.HandlerFunc(http.MethodGet, userExistsURL, errorMW(h.Exists))

	router.HandlerFunc(http.MethodPatch, userSubscriptionsURL, errorMW(h.UpdateSubscription))
	router.HandlerFunc(http.MethodPatch, userTelegramChatIDURL, errorMW(h.UpdateTelegramChatID))

	router.HandlerFunc(http.MethodPost, usersURL, errorMW(h.Create))
	router.HandlerFunc(http.MethodPatch, userURL, errorMW(h.Update))
	router.HandlerFunc(http.MethodGet, userURL, errorMW(h.FindOne))
	router.HandlerFunc(http.MethodGet, usersURL, errorMW(h.FindAll))
	router.HandlerFunc(http.MethodDelete, userURL, errorMW(h.Delete))

	// Admin alias: GET /admin/users → FindAll
	router.HandlerFunc(http.MethodGet, "/admin/users", errorMW(h.FindAll))

	// ── Admin: статистика ────────────────────────────────────────────────────────────
	router.HandlerFunc(http.MethodGet, "/admin/user-stats", errorMW(h.AdminUserStats))

	// Admin: блокировка/разблокировка пользователей
	router.HandlerFunc(http.MethodPatch, "/admin/users/:userId/block", errorMW(h.BlockUser))
	router.HandlerFunc(http.MethodPatch, "/admin/users/:userId/unblock", errorMW(h.UnblockUser))

	router.HandlerFunc(http.MethodPost, registerURL, errorMW(h.RegisterUser))
	router.HandlerFunc(http.MethodPost, loginURL, errorMW(h.ValidateCredentials))
	router.HandlerFunc(http.MethodPost, oauthRegisterURL, errorMW(h.RegisterOAuthUser))

	// ── Дружба ──────────────────────────────────────────────────────────
	router.HandlerFunc(http.MethodGet, userFriendsURL, errorMW(h.GetFriends))
	router.HandlerFunc(http.MethodPost, userFriendsURL, errorMW(h.SendFriendRequest)) // POST /friends — отправить заявку
	router.HandlerFunc(http.MethodGet, userFriendInboxURL, errorMW(h.GetFriendRequests))
	router.HandlerFunc(http.MethodPost, userFriendAcceptURL, errorMW(h.AcceptFriendRequest))
	router.HandlerFunc(http.MethodPost, userFriendRejectURL, errorMW(h.RejectFriendRequest))
	router.HandlerFunc(http.MethodDelete, userFriendDeleteURL, errorMW(h.RemoveFriend))

	// ── Сообщения ────────────────────────────────────────────────────────
	router.HandlerFunc(http.MethodPost, userMessagesURL, errorMW(h.SendMessage))
	router.HandlerFunc(http.MethodGet, userMessagesURL, errorMW(h.GetMessages))
	router.HandlerFunc(http.MethodPatch, userMessagesReadURL, errorMW(h.MarkMessagesRead))
	router.HandlerFunc(http.MethodGet, userUnreadCountURL, errorMW(h.GetUnreadCount))
	router.HandlerFunc(http.MethodGet, userUnreadPerFriendURL, errorMW(h.GetUnreadCountsPerFriend))
	router.HandlerFunc(http.MethodPatch, userMessageActionURL, errorMW(h.EditMessage))
	router.HandlerFunc(http.MethodDelete, userMessageActionURL, errorMW(h.DeleteMessage))

	// ── Поиск пользователя по email ──────────────────────────────────────
	router.HandlerFunc(http.MethodGet, userFindByEmailURL, errorMW(h.FindByEmail))

	// ── Смена пароля (авторизован) ────────────────────────────────────────
	router.HandlerFunc(http.MethodPatch, userChangePasswordURL, errorMW(h.ChangePassword))

	// ── Сброс пароля (публичные, без JWT) ────────────────────────────────
	router.HandlerFunc(http.MethodPost, userForgotPasswordURL, errorMW(h.ForgotPasswordHandler))
	router.HandlerFunc(http.MethodPost, userResetPasswordURL, errorMW(h.ResetPasswordHandler))

	// ── WebSocket (без подписи — gateway уже проверил JWT) ────────────────
	router.HandlerFunc(http.MethodGet, userWSURL, h.HandleWS)

	// ── Admin: просмотр чата между двумя пользователями ───────────────────
	router.HandlerFunc(http.MethodGet, "/admin/users/:userId/chat/:friendId", errorMW(h.AdminGetChat))
	// История версий одного сообщения
	router.HandlerFunc(http.MethodGet, "/admin/messages/:messageId/history", errorMW(h.AdminGetMessageHistory))

	// ── Internal: WS-уведомление от tasks-service ────────────────────────
	// Без подписи — принимаем только из внутренней сети Docker
	router.HandlerFunc(http.MethodPost, "/internal/ws-notify", errorMW(h.InternalWsNotify))
}

func (h *handler) FindByEmail(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	email := params.ByName("email")
	if email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return nil
	}
	user, err := h.service.FindUserByEmail(r.Context(), email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	if user == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(user)
}

func (h *handler) UpdateTelegramChatID(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	userID := params.ByName("uuid")
	requesterID := r.Header.Get("X-User-ID")
	role := r.Header.Get("X-User-Role")
	if role != "admin" && requesterID != userID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}

	var userRequest model.UserTODO
	if err := json.NewDecoder(r.Body).Decode(&userRequest); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return err
	}

	chatID := userRequest.TelegramChatID

	ctx := r.Context()

	err := h.service.UpdateTelegramChatID(ctx, userID, chatID)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *handler) UpdateSubscription(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	userID := params.ByName("uuid")
	requesterID := r.Header.Get("X-User-ID")
	role := r.Header.Get("X-User-Role")
	if role != "admin" && requesterID != userID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}

	ctx := r.Context()
	err := h.service.UpdateSubscription(ctx, userID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *handler) RegisterUser(w http.ResponseWriter, r *http.Request) error {
	var registerReq model.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&registerReq); err != nil {
		http.Error(w, "Некорректный формат запроса", http.StatusBadRequest)
		return nil
	}

	user := model.UserTODO{
		Email:    registerReq.Email,
		Name:     registerReq.Name,
		Password: registerReq.Password,
	}

	ctx := r.Context()
	userID, err := h.service.CreateUser(ctx, user)
	if err != nil {
		if errors.Is(err, errs.ErrUserAlreadyExists) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("Пользователь с таким email уже зарегистрирован"))
			return nil
		}

		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return nil
	}

	// Получаем созданного пользователя
	createdUser, err := h.service.FindOneUser(ctx, userID)
	if err != nil {
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return nil
	}

	createdUser.Password = ""
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": createdUser.Id})
	return nil
}

// RegisterOAuthUser — POST /user/oauth-register — создание пользователя через OAuth (без пароля).
// Вызывается только из auth-service (проверка X-Service-Name: auth-service).
func (h *handler) RegisterOAuthUser(w http.ResponseWriter, r *http.Request) error {
	var req model.OAuthRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Некорректный формат запроса", http.StatusBadRequest)
		return nil
	}
	if req.Email == "" || req.Name == "" {
		http.Error(w, "Email и имя обязательны", http.StatusBadRequest)
		return nil
	}

	ctx := r.Context()
	userID, err := h.service.CreateOAuthUser(ctx, req.Email, req.Name)
	if err != nil {
		if errors.Is(err, errs.ErrUserAlreadyExists) {
			// Проверяем — это OAuth пользователь или обычный?
			existingUser, findErr := h.service.FindUserByEmailFull(ctx, req.Email)
			if findErr != nil || existingUser == nil {
				http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
				return nil
			}
			if !existingUser.IsOAuth {
				// Email занят обычным аккаунтом — запрещаем вход через OAuth
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte("Этот email уже зарегистрирован с паролем"))
				return nil
			}
			// OAuth пользователь повторно входит — возвращаем его ID с кодом 200
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(existingUser.Id)
			return nil
		}
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return nil
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(userID)
	return nil
}

// ValidateCredentials - проверка email и пароля (вызывается из auth service при логине)
func (h *handler) ValidateCredentials(w http.ResponseWriter, r *http.Request) error {
	var credentials model.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Некорректный формат запроса"))
		return nil
	}

	ctx := r.Context()

	response, err := h.service.ValidateCredentials(ctx, credentials.Email, credentials.Password)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorMsg := "Internal err"

		switch {
		case errors.Is(err, errs.ErrUserNotFound):
			statusCode = http.StatusNotFound
			errorMsg = "Not found"
		case errors.Is(err, errs.ErrMissingData):
			statusCode = http.StatusUnauthorized
			errorMsg = "Unauthorized"
		case errors.Is(err, errs.ErrUserBlocked):
			statusCode = http.StatusForbidden
			errorMsg = "Ваш аккаунт заблокирован по решению руководства"
		}

		w.WriteHeader(statusCode)
		w.Write([]byte(errorMsg))
		return nil
	}

	// Успех
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
	return nil
}

func (h *handler) Create(w http.ResponseWriter, r *http.Request) error {
	var userDTO model.UserCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&userDTO); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
	}

	user := model.UserTODO{
		Email:    userDTO.Email,
		Name:     userDTO.Name,
		Password: userDTO.Password,
	}

	ctx := r.Context()
	userID, err := h.service.CreateUser(ctx, user)
	if err != nil {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return nil
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": userID})
	return nil
}

func (h *handler) Update(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	id := params.ByName("uuid")
	if id == "" {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return nil
	}
	requesterID := r.Header.Get("X-User-ID")
	role := r.Header.Get("X-User-Role")
	if role != "admin" && requesterID != id {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}

	// Декодируем JSON-данные из тела запроса в нашу новую структуру
	var userRequest model.UserUpdateDTO
	if err := json.NewDecoder(r.Body).Decode(&userRequest); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return err
	}

	// Обновляем пользователя в базе данных, передавая указатели
	_, err := h.service.UpdateUser(r.Context(), id, userRequest)

	// Обработка ошибок
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return err
	}

	// Отправляем успешный ответ с сообщением об успешном обновлении
	response := map[string]string{"message": "successful update"}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
	return nil
}

func (h *handler) Delete(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	id := params.ByName("uuid")
	if id == "" {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return nil
	}
	requesterID := r.Header.Get("X-User-ID")
	role := r.Header.Get("X-User-Role")
	if role != "admin" && requesterID != id {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}
	ctx := r.Context()
	_, err := h.service.DeleteUser(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return err
	}
	response := map[string]string{"message": "successful delete"}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
	return nil
}

func (h *handler) FindAll(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	users, err := h.service.FindAllUser(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "No users found", http.StatusNotFound)
			return err
		} else {
			http.Error(w, "Error finding users", http.StatusInternalServerError)
			return err
		}
	}

	if len(users) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	allBytes, err := json.Marshal(users)
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusOK)
	w.Write(allBytes)

	return nil
}

func (h *handler) FindOne(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	id := params.ByName("uuid")
	if id == "" {
		http.Error(w, "Identifier is required", http.StatusBadRequest)
		return nil
	}

	ctx := r.Context()
	user, err := h.service.FindOneUser(ctx, id)
	if err != nil {
		if errors.Is(err, errs.ErrUserNotFound) {
			http.Error(w, "User not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to retrieve user", http.StatusInternalServerError)
			return err
		}
		return nil
	}

	// Установим заголовок и отправим ответ
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(user)
	if err != nil {
		return err
	}

	return nil
}

func (h *handler) Exists(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	email := params.ByName("email")
	if email == "" {
		http.Error(w, "Email обязателен", http.StatusBadRequest)
		return nil
	}

	ctx := r.Context()
	err := h.service.Exists(ctx, email)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if err != nil {
		if errors.Is(err, errs.ErrUserAlreadyExists) {
			// Определяем — OAuth пользователь или обычный — чтобы дать правильное сообщение
			existingUser, findErr := h.service.FindUserByEmailFull(ctx, email)
			if findErr == nil && existingUser != nil && existingUser.IsOAuth {
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte("oauth_conflict"))
				return nil
			}
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("email_taken"))
			return nil
		}

		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Внутренняя ошибка сервера"))
		return nil
	}

	// Пользователь не существует
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(""))
	return nil
}

// ── Хендлеры дружбы ───────────────────────────────────────────────────────

func (h *handler) SendFriendRequest(w http.ResponseWriter, r *http.Request) error {
	// X-User-ID устанавливает gateway из JWT — клиент не может подделать
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil
	}
	var dto model.FriendActionDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil || dto.AddresseeID == "" {
		http.Error(w, "addressee_id required", http.StatusBadRequest)
		return nil
	}
	if err := h.service.SendFriendRequest(r.Context(), userID, dto.AddresseeID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}
	w.WriteHeader(http.StatusCreated)
	return nil
}

func (h *handler) AcceptFriendRequest(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	userID := r.Header.Get("X-User-ID")
	friendID := params.ByName("fid")
	if err := h.service.AcceptFriendRequest(r.Context(), userID, friendID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

func (h *handler) RejectFriendRequest(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	userID := r.Header.Get("X-User-ID")
	friendID := params.ByName("fid")
	if err := h.service.RejectFriendRequest(r.Context(), userID, friendID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

func (h *handler) RemoveFriend(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	userID := r.Header.Get("X-User-ID")
	friendID := params.ByName("fid")
	if err := h.service.RemoveFriend(r.Context(), userID, friendID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

func (h *handler) GetFriends(w http.ResponseWriter, r *http.Request) error {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		params := httprouter.ParamsFromContext(r.Context())
		userID = params.ByName("uuid")
	}
	friends, err := h.service.GetFriends(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	if friends == nil {
		friends = []model.FriendInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(friends)
}

func (h *handler) GetFriendRequests(w http.ResponseWriter, r *http.Request) error {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		params := httprouter.ParamsFromContext(r.Context())
		userID = params.ByName("uuid")
	}
	reqs, err := h.service.GetFriendRequests(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	if reqs == nil {
		reqs = []model.FriendRequest{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(reqs)
}

// ── Хендлеры сообщений ────────────────────────────────────────────────────

func (h *handler) SendMessage(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	userID := r.Header.Get("X-User-ID")
	friendID := params.ByName("fid")
	isSystem := r.Header.Get("X-Is-System") == "true"
	var dto model.SendMessageDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil || dto.Content == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return nil
	}
	messageID, err := h.service.SendMessage(r.Context(), userID, friendID, dto.Content, isSystem)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	return json.NewEncoder(w).Encode(map[string]string{"message_id": messageID})
}

func (h *handler) EditMessage(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	userID := r.Header.Get("X-User-ID")
	messageID := params.ByName("fid")
	var dto model.EditMessageDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil || strings.TrimSpace(dto.Content) == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return nil
	}
	msg, err := h.service.EditMessage(r.Context(), messageID, userID, strings.TrimSpace(dto.Content))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(msg)
}

func (h *handler) DeleteMessage(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	userID := r.Header.Get("X-User-ID")
	messageID := params.ByName("fid")
	if err := h.service.DeleteMessage(r.Context(), messageID, userID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *handler) GetMessages(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	userID := r.Header.Get("X-User-ID")
	friendID := params.ByName("fid")
	before := r.URL.Query().Get("before")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscan(l, &limit)
	}
	msgs, err := h.service.GetMessages(r.Context(), userID, friendID, before, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}
	if msgs == nil {
		msgs = []model.Message{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(msgs)
}

func (h *handler) MarkMessagesRead(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	userID := r.Header.Get("X-User-ID")
	friendID := params.ByName("fid")
	if err := h.service.MarkMessagesRead(r.Context(), userID, friendID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

func (h *handler) GetUnreadCount(w http.ResponseWriter, r *http.Request) error {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		params := httprouter.ParamsFromContext(r.Context())
		userID = params.ByName("uuid")
	}
	count, err := h.service.GetUnreadCount(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(map[string]int{"count": count})
}

func (h *handler) GetUnreadCountsPerFriend(w http.ResponseWriter, r *http.Request) error {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		params := httprouter.ParamsFromContext(r.Context())
		userID = params.ByName("uuid")
	}
	counts, err := h.service.GetUnreadCountsPerFriend(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	if counts == nil {
		counts = map[string]int{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(counts)
}

// ── Admin: блокировка пользователей ──────────────────────────────────────

func (h *handler) BlockUser(w http.ResponseWriter, r *http.Request) error {
	if r.Header.Get("X-User-Role") != "admin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}
	params := httprouter.ParamsFromContext(r.Context())
	targetID := params.ByName("userId")
	if targetID == "" {
		http.Error(w, "user id required", http.StatusBadRequest)
		return nil
	}

	// Запрет блокировки самого себя
	callerID := r.Header.Get("X-User-ID")
	if callerID != "" && callerID == targetID {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"нельзя заблокировать самого себя"}`, http.StatusForbidden)
		return nil
	}

	// Запрет блокировки другого администратора
	targetUser, err := h.service.FindOneUser(r.Context(), targetID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return nil
	}
	if targetUser.Role == "admin" {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"нельзя заблокировать администратора"}`, http.StatusForbidden)
		return nil
	}

	if err := h.service.BlockUser(r.Context(), targetID); err != nil {
		http.Error(w, "failed to block user", http.StatusInternalServerError)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *handler) UnblockUser(w http.ResponseWriter, r *http.Request) error {
	if r.Header.Get("X-User-Role") != "admin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}
	params := httprouter.ParamsFromContext(r.Context())
	userID := params.ByName("userId")
	if userID == "" {
		http.Error(w, "user id required", http.StatusBadRequest)
		return nil
	}
	if err := h.service.UnblockUser(r.Context(), userID); err != nil {
		http.Error(w, "failed to unblock user", http.StatusInternalServerError)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ── Хендлеры пароля ───────────────────────────────────────────────────────

// ChangePassword — PATCH /users/:uuid/password — смена пароля в профиле
func (h *handler) ChangePassword(w http.ResponseWriter, r *http.Request) error {
	params := httprouter.ParamsFromContext(r.Context())
	userID := params.ByName("uuid")
	requesterID := r.Header.Get("X-User-ID")
	if requesterID == "" || requesterID != userID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}
	var dto model.ChangePasswordDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return nil
	}
	if dto.OldPassword == "" || dto.NewPassword == "" {
		http.Error(w, "old_password and new_password required", http.StatusBadRequest)
		return nil
	}
	if err := h.service.ChangePassword(r.Context(), userID, dto.OldPassword, dto.NewPassword); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

// ForgotPasswordHandler — POST /user/forgot-password — запрос кода сброса
func (h *handler) ForgotPasswordHandler(w http.ResponseWriter, r *http.Request) error {
	var req model.ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return nil
	}
	if err := h.service.ForgotPassword(r.Context(), req.Email); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message":"код отправлен на почту"}`))
	return nil
}

// ResetPasswordHandler — POST /user/reset-password — установка нового пароля по коду
func (h *handler) ResetPasswordHandler(w http.ResponseWriter, r *http.Request) error {
	var req model.ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return nil
	}
	if req.Email == "" || req.Code == "" || req.NewPassword == "" {
		http.Error(w, "email, code and new_password required", http.StatusBadRequest)
		return nil
	}
	if err := h.service.ResetPassword(r.Context(), req.Email, req.Code, req.NewPassword); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message":"пароль успешно изменён"}`))
	return nil
}

// ── Admin: чат между двумя пользователями ────────────────────────────────

// AdminGetChat — GET /admin/users/:userId/chat/:friendId
// Возвращает историю сообщений между двумя пользователями без проверки дружбы.
func (h *handler) AdminGetChat(w http.ResponseWriter, r *http.Request) error {
	if r.Header.Get("X-User-Role") != "admin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}
	params := httprouter.ParamsFromContext(r.Context())
	userID := params.ByName("userId")
	friendID := params.ByName("friendId")
	before := r.URL.Query().Get("before")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscan(l, &limit)
	}

	msgs, err := h.service.AdminGetChat(r.Context(), userID, friendID, before, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	if msgs == nil {
		msgs = []model.Message{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(msgs)
}

// AdminGetMessageHistory — GET /admin/messages/:messageId/history
// Возвращает все версии сообщения: created, edited, deleted.
func (h *handler) AdminGetMessageHistory(w http.ResponseWriter, r *http.Request) error {
	if r.Header.Get("X-User-Role") != "admin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}
	params := httprouter.ParamsFromContext(r.Context())
	messageID := params.ByName("messageId")
	if messageID == "" {
		http.Error(w, "messageId required", http.StatusBadRequest)
		return nil
	}
	entries, err := h.service.AdminGetMessageHistory(r.Context(), messageID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	if entries == nil {
		entries = []model.MessageHistoryEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(entries)
}

// AdminUserStats — GET /admin/users/stats?from=&to=
// Возвращает статистику по событиям подписок.
func (h *handler) AdminUserStats(w http.ResponseWriter, r *http.Request) error {
	if r.Header.Get("X-User-Role") != "admin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	stats, err := h.service.GetUserStats(r.Context(), from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(stats)
}

// InternalWsNotify — POST /internal/ws-notify
// Принимает WS-событие от tasks-service и рассылает через Hub.
func (h *handler) InternalWsNotify(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		UserID    string      `json:"user_id"`
		EventType string      `json:"event_type"`
		Data      interface{} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" || req.EventType == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return nil
	}

	if h.service.Hub != nil {
		h.service.Hub.Send(req.UserID, ws.Event{Type: req.EventType, Data: req.Data})
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

// ── WebSocket хендлер ─────────────────────────────────────────────────────

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Gateway уже проверил Origin — доверяем всем источникам
	CheckOrigin: func(r *http.Request) bool { return true },
}

// HandleWS — обновляет соединение до WebSocket и регистрирует клиента в Hub.
// X-User-ID устанавливает gateway из JWT — клиент не может подделать.
func (h *handler) HandleWS(w http.ResponseWriter, r *http.Request) {
	params := httprouter.ParamsFromContext(r.Context())
	uuidParam := params.ByName("uuid")

	userID := r.Header.Get("X-User-ID")
	if userID == "" || userID != uuidParam {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if h.service.Hub == nil {
		http.Error(w, "ws not available", http.StatusServiceUnavailable)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	client := h.service.Hub.Register(userID, conn)
	client.ReadPump()
}
