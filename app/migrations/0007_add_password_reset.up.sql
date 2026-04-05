CREATE TABLE IF NOT EXISTS password_reset_tokens (
    email      VARCHAR(100) PRIMARY KEY,
    code       VARCHAR(4)   NOT NULL,
    expires_at TIMESTAMP    NOT NULL
);
