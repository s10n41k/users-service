package signature

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func Middleware(h http.HandlerFunc) http.HandlerFunc {
	secret := os.Getenv("GATEWAY_SIGN")

	if secret == "" {
		return nil
	}

	return func(w http.ResponseWriter, r *http.Request) {

		// 1. Пропускаем публичные маршруты
		if isPublicRoute(r.URL.Path) {
			h(w, r)
			return
		}

		// 2. РАЗРЕШАЕМ ВСЕ ВНУТРЕННИЕ СЕРВИСЫ БЕЗ ПОДПИСИ
		allowedInternalServices := []string{
			"auth-service",
			"tasks-service",
			"analytics-service",
			"gateway",
		}

		callerService := r.Header.Get("X-Service-Name")
		for _, service := range allowedInternalServices {
			if callerService == service {
				fmt.Printf("[SIGNATURE DEBUG] Allowing internal service: %s\n", callerService)

				// Добавляем информацию в контекст
				ctx := r.Context()
				ctx = context.WithValue(ctx, "service_name", callerService)
				ctx = context.WithValue(ctx, "user_id", r.Header.Get("X-User-ID"))
				ctx = context.WithValue(ctx, "session_id", r.Header.Get("X-Session-ID"))

				if roles := r.Header.Get("X-User-Roles"); roles != "" {
					ctx = context.WithValue(ctx, "user_roles", strings.Split(roles, ","))
				}

				r = r.WithContext(ctx)
				h(w, r)
				return
			}
		}

		// 3. ВНЕШНИЕ ВЫЗОВЫ (от Gateway) - ПРОВЕРЯЕМ ПОДПИСЬ

		// Получаем заголовки
		signature := r.Header.Get("X-Signature")
		timestampStr := r.Header.Get("X-Timestamp")
		serviceName := r.Header.Get("X-Service-Name")

		// Проверяем базовые требования
		if signature == "" || timestampStr == "" {
			http.Error(w, `{"error": "Signature required"}`, http.StatusUnauthorized)
			return
		}

		if serviceName != "gateway" {
			http.Error(w, `{"error": "Invalid service source"}`, http.StatusUnauthorized)
			return
		}

		// Проверяем timestamp
		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error": "Invalid timestamp"}`, http.StatusUnauthorized)
			return
		}

		// Проверяем свежесть запроса (5 минут)
		if time.Since(time.Unix(timestamp, 0)) > 5*time.Minute {
			http.Error(w, `{"error": "Request too old"}`, http.StatusUnauthorized)
			return
		}

		// Воссоздаем подпись для проверки
		userID := r.Header.Get("X-User-ID")
		sessionID := r.Header.Get("X-Session-ID")

		// Собираем данные как в Gateway
		parts := []string{r.Method, r.URL.Path, timestampStr}
		if userID != "" {
			parts = append(parts, userID)
		}
		if sessionID != "" {
			parts = append(parts, sessionID)
		}

		dataToSign := strings.Join(parts, "|")

		fmt.Printf("[SIGNATURE DEBUG] Data to sign: %s\n", dataToSign)

		// Создаем HMAC для проверки
		hmacHash := hmac.New(sha256.New, []byte(secret))
		hmacHash.Write([]byte(dataToSign))
		expectedSignature := hex.EncodeToString(hmacHash.Sum(nil))

		// Сравниваем подписи
		if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
			fmt.Printf("[SIGNATURE DEBUG] Signature mismatch! Expected: %s, Got: %s\n",
				expectedSignature[:10]+"...", signature[:10]+"...")
			http.Error(w, `{"error": "Invalid signature"}`, http.StatusUnauthorized)
			return
		}

		// Подпись верна - добавляем информацию в контекст
		ctx := r.Context()
		ctx = context.WithValue(ctx, "signature_verified", true)
		ctx = context.WithValue(ctx, "service_name", "gateway")
		ctx = context.WithValue(ctx, "user_id", userID)
		ctx = context.WithValue(ctx, "session_id", sessionID)

		if roles := r.Header.Get("X-User-Roles"); roles != "" {
			ctx = context.WithValue(ctx, "user_roles", strings.Split(roles, ","))
		}

		r = r.WithContext(ctx)
		h(w, r)
	}
}

// isPublicRoute определяет публичные маршруты
func isPublicRoute(path string) bool {
	publicRoutes := []string{
		"/health",
		"/metrics",
		"/ready",
		"/live",
		"/docs",
		"/swagger",
		"/favicon.ico",
	}

	for _, route := range publicRoutes {
		if strings.HasPrefix(path, route) {
			return true
		}
	}
	return false
}
