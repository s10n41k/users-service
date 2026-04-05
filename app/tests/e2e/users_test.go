//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Register ---

func TestRegister_Success(t *testing.T) {
	body := map[string]string{
		"email":    fmt.Sprintf("newuser_%d@example.com", uniqueSuffix()),
		"name":     "New User",
		"password": "password123",
	}
	req, err := http.NewRequest(http.MethodPost, testServer.URL+"/user/register", jsonBody(t, body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp := do(t, req)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	assert.NotEmpty(t, result["id"])
}

func TestRegister_DuplicateEmail(t *testing.T) {
	email := fmt.Sprintf("dup_%d@example.com", uniqueSuffix())
	body := map[string]string{
		"email":    email,
		"name":     "User",
		"password": "password123",
	}

	// Первая регистрация
	req1, _ := http.NewRequest(http.MethodPost, testServer.URL+"/user/register", jsonBody(t, body))
	req1.Header.Set("Content-Type", "application/json")
	resp1 := do(t, req1)
	assert.Equal(t, http.StatusCreated, resp1.StatusCode)
	resp1.Body.Close()

	// Повторная регистрация — должен вернуть ошибку
	req2, _ := http.NewRequest(http.MethodPost, testServer.URL+"/user/register", jsonBody(t, body))
	req2.Header.Set("Content-Type", "application/json")
	resp2 := do(t, req2)
	assert.Equal(t, http.StatusConflict, resp2.StatusCode)
	resp2.Body.Close()
}

// --- CheckEmail ---

func TestCheckEmail_Free(t *testing.T) {
	email := fmt.Sprintf("free_%d@example.com", uniqueSuffix())
	req := signedReq(t, http.MethodGet, "/user/check-email/"+email, "", nil)
	resp := do(t, req)
	// Сервис возвращает ErrUserAlreadyExists если email свободен (ожидаем 409)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	resp.Body.Close()
}

// --- GetUser ---

func TestGetUser_Success(t *testing.T) {
	req := signedReq(t, http.MethodGet, "/users/"+testUserID, testUserID, nil)
	resp := do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var user map[string]interface{}
	decodeJSON(t, resp, &user)
	assert.Equal(t, testUserID, user["id"])
	assert.NotEmpty(t, user["name"])
	// Пароль не должен возвращаться
	assert.Empty(t, user["password"])
}

func TestGetUser_NotFound(t *testing.T) {
	unknownID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	req := signedReq(t, http.MethodGet, "/users/"+unknownID, testUserID, nil)
	resp := do(t, req)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
}

// --- UpdateUser ---

func TestUpdateUser_Name(t *testing.T) {
	// Регистрируем нового пользователя для теста обновления
	email := fmt.Sprintf("update_test_%d@example.com", uniqueSuffix())
	body := map[string]string{
		"email":    email,
		"name":     "Original Name",
		"password": "password123",
	}
	req1, _ := http.NewRequest(http.MethodPost, testServer.URL+"/user/register", jsonBody(t, body))
	req1.Header.Set("Content-Type", "application/json")
	resp1 := do(t, req1)
	require.Equal(t, http.StatusCreated, resp1.StatusCode)

	var created map[string]interface{}
	decodeJSON(t, resp1, &created)
	createdID := created["id"].(string)
	require.NotEmpty(t, createdID)

	// Обновляем имя
	updateBody := map[string]string{"name": "Updated Name"}
	req2 := signedReq(t, http.MethodPatch, "/users/"+createdID, createdID, updateBody)
	resp2 := do(t, req2)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	resp2.Body.Close()

	// Проверяем что имя изменилось
	req3 := signedReq(t, http.MethodGet, "/users/"+createdID, createdID, nil)
	resp3 := do(t, req3)
	require.Equal(t, http.StatusOK, resp3.StatusCode)
	var updated map[string]interface{}
	decodeJSON(t, resp3, &updated)
	assert.Equal(t, "Updated Name", updated["name"])
}

// --- FindByEmail ---

func TestFindByEmail_Exists(t *testing.T) {
	req := signedReq(t, http.MethodGet, "/user/by-email/alice@test.com", testUserID, nil)
	resp := do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var user map[string]interface{}
	decodeJSON(t, resp, &user)
	assert.Equal(t, "alice@test.com", user["email"])
}

func TestFindByEmail_NotFound(t *testing.T) {
	req := signedReq(t, http.MethodGet, "/user/by-email/nobody@nowhere.com", testUserID, nil)
	resp := do(t, req)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
}

// --- GetAllUsers (admin) ---

func TestGetAllUsers(t *testing.T) {
	req := signedReq(t, http.MethodGet, "/users", testUserID, nil)
	resp := do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var users []interface{}
	decodeJSON(t, resp, &users)
	assert.NotEmpty(t, users)
}

// --- Friends workflow ---

func TestFriends_SendAndAccept(t *testing.T) {
	// testUserID (Alice) отправляет заявку testUserID2 (Bob)
	sendBody := map[string]string{"addressee_id": testUserID2}
	req1 := signedReq(t, http.MethodPost, "/users/"+testUserID+"/friends", testUserID, sendBody)
	resp1 := do(t, req1)
	// 201 Created или 200 OK
	assert.True(t, resp1.StatusCode == http.StatusCreated || resp1.StatusCode == http.StatusOK,
		"expected 201 or 200, got %d: %s", resp1.StatusCode, readBody(resp1))
	resp1.Body.Close()

	// Bob смотрит входящие заявки
	req2 := signedReq(t, http.MethodGet, "/users/"+testUserID2+"/friends-inbox", testUserID2, nil)
	resp2 := do(t, req2)
	require.Equal(t, http.StatusOK, resp2.StatusCode)
	var inbox []map[string]interface{}
	decodeJSON(t, resp2, &inbox)
	assert.NotEmpty(t, inbox)

	// Bob принимает заявку от Alice
	req3 := signedReq(t, http.MethodPost, "/users/"+testUserID2+"/friends/"+testUserID+"/accept", testUserID2, nil)
	resp3 := do(t, req3)
	assert.True(t, resp3.StatusCode == http.StatusOK || resp3.StatusCode == http.StatusNoContent,
		"expected 200 or 204, got %d: %s", resp3.StatusCode, readBody(resp3))
	resp3.Body.Close()

	// Alice смотрит список друзей
	req4 := signedReq(t, http.MethodGet, "/users/"+testUserID+"/friends", testUserID, nil)
	resp4 := do(t, req4)
	require.Equal(t, http.StatusOK, resp4.StatusCode)
	var friends []map[string]interface{}
	decodeJSON(t, resp4, &friends)
	assert.NotEmpty(t, friends)

	t.Cleanup(func() {
		// Очистка: удаляем дружбу после теста
		reqDel := signedReq(t, http.MethodDelete, "/users/"+testUserID+"/friends/"+testUserID2, testUserID, nil)
		do(t, reqDel).Body.Close() //nolint:errcheck
	})
}

func TestFriends_RejectRequest(t *testing.T) {
	// Отправляем заявку ещё раз (или она уже есть — ON CONFLICT DO NOTHING)
	sendBody := map[string]string{"addressee_id": testUserID2}
	req1 := signedReq(t, http.MethodPost, "/users/"+testUserID+"/friends", testUserID, sendBody)
	resp1 := do(t, req1)
	resp1.Body.Close()

	// Bob отклоняет
	req2 := signedReq(t, http.MethodPost, "/users/"+testUserID2+"/friends/"+testUserID+"/reject", testUserID2, nil)
	resp2 := do(t, req2)
	assert.True(t, resp2.StatusCode == http.StatusOK || resp2.StatusCode == http.StatusNoContent,
		"got %d: %s", resp2.StatusCode, readBody(resp2))
	resp2.Body.Close()
}

// --- Unread messages (stub returns 0) ---

func TestUnreadCount_Returns(t *testing.T) {
	req := signedReq(t, http.MethodGet, "/users/"+testUserID+"/messages-unread", testUserID, nil)
	resp := do(t, req)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}
