// Package activity is the task audit log. Entries are written by the domain
// services (never by the browser) inside the same transaction as the change
// they describe, and they are append-only: there is no update or delete API,
// and a database trigger rejects modifications.
package activity

import (
	"context"
	"database/sql"

	"kanban/internal/database"
)

// Action types recorded in task_activities.action.
const (
	TaskCreated        = "task_created"
	TitleChanged       = "title_changed"
	DescriptionChanged = "description_changed"
	PriorityChanged    = "priority_changed"
	DueDateChanged     = "due_date_changed"
	Assigned           = "assigned"
	Reassigned         = "reassigned"
	Unassigned         = "unassigned"
	LabelsChanged      = "labels_changed"
	Moved              = "moved"
	Reordered          = "reordered"
	Completed          = "completed"
	Reopened           = "reopened"
	CommentAdded       = "comment_added"
	CommentEdited      = "comment_edited"
	CommentDeleted     = "comment_deleted"
	TaskDeleted        = "task_deleted"
)

// Entry is a new activity record.
type Entry struct {
	TaskID    int64
	BoardID   int64
	TaskTitle string
	// UserID is the acting user. Callers must take it from the authenticated
	// session, never from request input.
	UserID      int64
	Action      string
	Description string // phrased to follow the actor's name, e.g. "changed priority from Medium to High"
	OldValue    *string
	NewValue    *string
}

// Str is a convenience for building *string values.
func Str(s string) *string { return &s }

// Record appends an entry. Pass the *sql.Tx of the surrounding change so the
// entry is committed (or rolled back) together with it.
func Record(ctx context.Context, q database.DBTX, e Entry) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO task_activities
			(task_id, board_id, task_title, user_id, action, description, old_value, new_value, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TaskID, e.BoardID, e.TaskTitle, e.UserID, e.Action, e.Description, e.OldValue, e.NewValue, database.Now())
	return err
}

// Item is an activity entry joined with the actor's name for display.
type Item struct {
	ID          int64
	TaskID      sql.NullInt64
	BoardID     int64
	TaskTitle   string
	UserID      int64
	UserName    string
	Action      string
	Description string
	OldValue    sql.NullString
	NewValue    sql.NullString
	CreatedAt   database.Time
}

// Service reads the activity log.
type Service struct {
	DB *sql.DB
}

// ForTask returns a task's complete history, oldest first. Callers must
// check board access before calling.
func (s *Service) ForTask(ctx context.Context, taskID int64) ([]Item, error) {
	return s.query(ctx, `
		SELECT a.id, a.task_id, a.board_id, a.task_title, a.user_id, u.name, a.action,
		       a.description, a.old_value, a.new_value, a.created_at
		FROM task_activities a JOIN users u ON u.id = a.user_id
		WHERE a.task_id = ?
		ORDER BY a.created_at, a.id`, taskID)
}

// ForBoard returns the most recent activity across a board, newest first,
// including entries for tasks that have since been deleted.
func (s *Service) ForBoard(ctx context.Context, boardID int64, limit int) ([]Item, error) {
	return s.query(ctx, `
		SELECT a.id, a.task_id, a.board_id, a.task_title, a.user_id, u.name, a.action,
		       a.description, a.old_value, a.new_value, a.created_at
		FROM task_activities a JOIN users u ON u.id = a.user_id
		WHERE a.board_id = ?
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT ?`, boardID, limit)
}

func (s *Service) query(ctx context.Context, q string, args ...any) ([]Item, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.TaskID, &it.BoardID, &it.TaskTitle, &it.UserID, &it.UserName,
			&it.Action, &it.Description, &it.OldValue, &it.NewValue, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
