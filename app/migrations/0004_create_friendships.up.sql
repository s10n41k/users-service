CREATE TABLE IF NOT EXISTS friendships (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_id UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    addressee_id UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    status       VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT friendships_no_self CHECK (requester_id != addressee_id),
    CONSTRAINT friendships_unique  UNIQUE (requester_id, addressee_id)
    );

CREATE INDEX IF NOT EXISTS idx_fs_requester ON friendships(requester_id);
CREATE INDEX IF NOT EXISTS idx_fs_addressee ON friendships(addressee_id);
CREATE INDEX IF NOT EXISTS idx_fs_pending   ON friendships(addressee_id) WHERE status='pending';