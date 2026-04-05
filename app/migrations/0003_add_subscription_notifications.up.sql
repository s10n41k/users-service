CREATE TABLE subscription_notifications (
    user_id    UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    window_key VARCHAR(10) NOT NULL,
    sent_at    TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (user_id, window_key)
);