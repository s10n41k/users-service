//go:build e2e

package e2e

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var counter int64

// uniqueSuffix возвращает уникальный суффикс для изоляции тест-данных.
func uniqueSuffix() int64 {
	return atomic.AddInt64(&counter, 1)*1000 + time.Now().UnixNano()%1000
}

// jsonBody сериализует v в JSON и возвращает io.Reader.
func jsonBody(t *testing.T, v interface{}) io.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewReader(b)
}

// signedReq собирает HTTP-запрос с валидной HMAC-подписью gateway.
// userID передаётся через заголовок X-User-ID и включается в подпись.
func signedReq(t *testing.T, method, path, userID string, body interface{}) *http.Request {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, testServer.URL+path, bodyReader)
	require.NoError(t, err)

	ts := fmt.Sprintf("%d", time.Now().Unix())
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Service-Name", "gateway")
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	parts := []string{method, path, ts}
	if userID != "" {
		parts = append(parts, userID)
	}
	data := strings.Join(parts, "|")
	h := hmac.New(sha256.New, []byte(testGatewaySign))
	h.Write([]byte(data))
	req.Header.Set("X-Signature", hex.EncodeToString(h.Sum(nil)))

	return req
}

// signedReqWithRole добавляет заголовок X-User-Role для admin-эндпоинтов.
func signedReqWithRole(t *testing.T, method, path, userID, role string, body interface{}) *http.Request {
	t.Helper()
	req := signedReq(t, method, path, userID, body)
	req.Header.Set("X-User-Role", role)
	return req
}

// do выполняет запрос и возвращает ответ.
func do(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// decodeJSON читает тело ответа и декодирует JSON в dst.
func decodeJSON(t *testing.T, resp *http.Response, dst interface{}) {
	t.Helper()
	defer resp.Body.Close()
	require.NoError(t, json.NewDecoder(resp.Body).Decode(dst))
}

// readBody читает и возвращает тело ответа как строку.
func readBody(resp *http.Response) string {
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b))
}
