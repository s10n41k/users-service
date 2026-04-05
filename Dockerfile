ARG GO_VERSION=1.25

# =========================
# Stage 1 — build
# =========================
FROM golang:${GO_VERSION}-alpine AS builder

ENV GOTOOLCHAIN=local
ENV GOPROXY=https://proxy.golang.org,direct
ENV GONOSUMCHECK=*
ENV GONOSUMDB=*

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

ARG CACHEBUST=1
COPY . .

RUN ls -la /app/app/migrations/ || echo "No migrations found"
RUN CGO_ENABLED=0 GOOS=linux go build -o users-service ./app/cmd

# =========================
# Stage 2 — runtime
# =========================
FROM alpine:latest

RUN apk --no-cache add ca-certificates postgresql-client bash

WORKDIR /app

RUN mkdir -p /app/logs && chmod 755 /app/logs

COPY --from=builder /app/app/migrations /app/migrations
RUN ls -la /app/migrations/
COPY --from=builder /app/users-service .

EXPOSE 8080

CMD echo "Запускаем users-service..." && \
    echo "Ждем PostgreSQL..." && \
    until pg_isready -h ${DB_USERS_HOST} -p ${DB_USERS_PORT} -U ${DB_USERS_USERNAME} 2>/dev/null; do \
        echo "PostgreSQL недоступен - ждем..."; \
        sleep 2; \
    done && \
    echo "PostgreSQL доступен, применяем миграции..." && \
    for f in $(ls /app/migrations/*.up.sql | sort); do \
        echo "Применяем $f..."; \
        psql "postgresql://${DB_USERS_USERNAME}:${DB_USERS_PASSWORD}@${DB_USERS_HOST}:${DB_USERS_PORT}/${DB_USERS_DATABASE}" -f "$f"; \
    done && \
    echo "Миграции завершены, запускаем приложение..." && \
    ./users-service
