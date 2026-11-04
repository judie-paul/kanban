// Package notifications stores simple in-app notifications. They are created
// by the domain services in the same transaction as the triggering change
// and displayed when the user loads a page (no WebSockets needed).
package notifications

import (
	"context"
	"database/sql"
	"errors"

	"kanban/internal/apperr"

	"kanban/internal/database"
)

// New describes a notification to create.
type New struct {
	RecipientID int64
	ActorID     int64
	TaskID      int64
	BoardID     int64
	Message     string // phrased to follow the actor's name, e.g. "assigned you to “Fix login”"
}

// Create stores a notification. Notifying users about their own actions is
// pointless, so those are silently skipped.
func Create(ctx context.Context, q database.DBTX, n New) error {
	if n.RecipientID == 0 || n.RecipientID == n.ActorID {
		return nil
	}
	_, err := q.ExecContext(ctx, `
		INSERT INTO notifications (recipient_id, actor_id, task_id, board_id, message, is_read, created_at)
		VALUES (?, ?, ?, ?, ?, 0, ?)`,
		n.RecipientID, n.ActorID, nullID(n.TaskID), nullID(n.BoardID), n.Message, database.Now())
	return err
}

func nullID(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: id != 0}
}

// CreateMany notifies each distinct recipient once.
func CreateMany(ctx context.Context, q database.DBTX, recipients []int64, n New) error {
	seen := map[int64]bool{}
	for _, r := range recipients {
		if r == 0 || seen[r] {
			continue
		}
		seen[r] = true
		n.RecipientID = r
		if err := Create(ctx, q, n); err != nil {
			return err
		}
	}
	return nil
}

// Notification is a stored notification joined with display data.
type Notification struct {
	ID        int64
	ActorName string
	TaskID    sql.NullInt64
	BoardID   sql.NullInt64
	Message   string
	IsRead    bool
	CreatedAt database.Time
}

// Service reads and updates notifications.
type Service struct {
	DB *sql.DB
}

// List returns a user's notifications, newest first.
func (s *Service) List(ctx context.Context, userID int64, limit int) ([]Notification, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT n.id, u.name, n.task_id, n.board_id, n.message, n.is_read, n.created_at
		FROM notifications n JOIN users u ON u.id = n.actor_id
		WHERE n.recipient_id = ?
		ORDER BY n.created_at DESC, n.id DESC
		LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.ActorName, &n.TaskID, &n.BoardID, &n.Message, &n.IsRead, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UnreadCount returns the number of unread notifications.
func (s *Service) UnreadCount(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE recipient_id = ? AND is_read = 0`, userID).Scan(&n)
	return n, err
}

// MarkRead marks one notification as read. The recipient check means users
// can only touch their own notifications.
func (s *Service) MarkRead(ctx context.Context, userID, id int64) (Notification, error) {
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE notifications SET is_read = 1 WHERE id = ? AND recipient_id = ?`, id, userID); err != nil {
		return Notification{}, err
	}
	var n Notification
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, task_id, board_id, message, is_read FROM notifications WHERE id = ? AND recipient_id = ?`, id, userID).
		Scan(&n.ID, &n.TaskID, &n.BoardID, &n.Message, &n.IsRead)
	if errors.Is(err, sql.ErrNoRows) {
		return Notification{}, apperr.ErrNotFound
	}
	return n, err
}

// MarkAllRead marks every notification of the user as read.
func (s *Service) MarkAllRead(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE notifications SET is_read = 1 WHERE recipient_id = ? AND is_read = 0`, userID)
	return err
}
