CREATE TABLE user_subscriptions (
    user_id          UUID PRIMARY KEY REFERENCES users(user_id) ON DELETE CASCADE,
    has_subscription BOOLEAN   DEFAULT FALSE,
    subscribed_at    TIMESTAMP,
    expires_at       TIMESTAMP,
    telegram_chat_id BIGINT,
    telegram_linked_at TIMESTAMP,
    updated_at       TIMESTAMP DEFAULT NOW()
);