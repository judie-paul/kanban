-- Initial schema for the Kanban application.
-- All timestamps are UTC text in the fixed format YYYY-MM-DDTHH:MM:SSZ.
-- Due dates are calendar dates in the format YYYY-MM-DD.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    name          TEXT    NOT NULL,
    email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    created_at    TEXT    NOT NULL
);

-- Only a SHA-256 hash of the session token is stored, so a leaked database
-- cannot be used to hijack sessions.
CREATE TABLE sessions (
    token_hash TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE boards (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    owner_id    INTEGER NOT NULL REFERENCES users(id),
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL
);
CREATE INDEX idx_boards_owner ON boards(owner_id);

CREATE TABLE board_members (
    board_id INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role     TEXT    NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    added_at TEXT    NOT NULL,
    PRIMARY KEY (board_id, user_id)
);
CREATE INDEX idx_board_members_user ON board_members(user_id);

-- is_done marks a column whose tasks count as completed (e.g. "Done").
CREATE TABLE columns (
    id         INTEGER PRIMARY KEY,
    board_id   INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    position   INTEGER NOT NULL,
    is_done    INTEGER NOT NULL DEFAULT 0 CHECK (is_done IN (0, 1)),
    created_at TEXT    NOT NULL
);
CREATE INDEX idx_columns_board ON columns(board_id, position);

CREATE TABLE tasks (
    id           INTEGER PRIMARY KEY,
    board_id     INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    column_id    INTEGER NOT NULL REFERENCES columns(id),
    title        TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    priority     TEXT    NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
    due_date     TEXT,
    assignee_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    position     INTEGER NOT NULL,
    creator_id   INTEGER NOT NULL REFERENCES users(id),
    created_at   TEXT    NOT NULL,
    updated_at   TEXT    NOT NULL,
    completed_at TEXT
);
CREATE INDEX idx_tasks_board ON tasks(board_id);
CREATE INDEX idx_tasks_column ON tasks(column_id, position);
CREATE INDEX idx_tasks_assignee ON tasks(assignee_id);
CREATE INDEX idx_tasks_creator ON tasks(creator_id);
CREATE INDEX idx_tasks_updated ON tasks(updated_at);

CREATE TABLE labels (
    id       INTEGER PRIMARY KEY,
    board_id INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    name     TEXT    NOT NULL,
    color    TEXT    NOT NULL,
    UNIQUE (board_id, name)
);

CREATE TABLE task_labels (
    task_id  INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    label_id INTEGER NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, label_id)
);
CREATE INDEX idx_task_labels_label ON task_labels(label_id);

CREATE TABLE comments (
    id         INTEGER PRIMARY KEY,
    task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    author_id  INTEGER NOT NULL REFERENCES users(id),
    content    TEXT    NOT NULL,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);
CREATE INDEX idx_comments_task ON comments(task_id, created_at);

-- The audit log. task_id becomes NULL if the task is deleted, but the entry
-- itself survives (with a snapshot of the task title) so deletions remain
-- accountable. Entries disappear only when the whole board is deleted.
CREATE TABLE task_activities (
    id          INTEGER PRIMARY KEY,
    task_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
    board_id    INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    task_title  TEXT    NOT NULL,
    user_id     INTEGER NOT NULL REFERENCES users(id),
    action      TEXT    NOT NULL,
    description TEXT    NOT NULL,
    old_value   TEXT,
    new_value   TEXT,
    created_at  TEXT    NOT NULL
);
CREATE INDEX idx_task_activities_task ON task_activities(task_id, created_at);
CREATE INDEX idx_task_activities_board ON task_activities(board_id, created_at);

-- Activity history is append-only: the only column that may ever change is
-- task_id (set to NULL by the foreign key when a task is deleted).
CREATE TRIGGER task_activities_append_only
BEFORE UPDATE OF id, board_id, task_title, user_id, action, description, old_value, new_value, created_at
ON task_activities
BEGIN
    SELECT RAISE(ABORT, 'task activity history is append-only');
END;

CREATE TABLE notifications (
    id           INTEGER PRIMARY KEY,
    recipient_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_id     INTEGER NOT NULL REFERENCES users(id),
    task_id      INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
    board_id     INTEGER REFERENCES boards(id) ON DELETE CASCADE,
    message      TEXT    NOT NULL,
    is_read      INTEGER NOT NULL DEFAULT 0 CHECK (is_read IN (0, 1)),
    created_at   TEXT    NOT NULL
);
CREATE INDEX idx_notifications_recipient ON notifications(recipient_id, is_read, created_at);
