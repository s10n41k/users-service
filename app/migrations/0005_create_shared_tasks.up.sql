CREATE TABLE IF NOT EXISTS shared_tasks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    proposer_id  UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    addressee_id UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    title        TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    status       VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS shared_subtasks (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shared_task_id UUID NOT NULL REFERENCES shared_tasks(id) ON DELETE CASCADE,
    title          TEXT NOT NULL,
    assignee_id    UUID NOT NULL REFERENCES users(user_id),
    is_done        BOOLEAN NOT NULL DEFAULT FALSE,
    order_num      INT NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_st_proposer   ON shared_tasks(proposer_id);
CREATE INDEX IF NOT EXISTS idx_st_addressee  ON shared_tasks(addressee_id);
CREATE INDEX IF NOT EXISTS idx_sst_task      ON shared_subtasks(shared_task_id);
