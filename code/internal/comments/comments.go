// Package comments implements task comments. Adding, editing and deleting
// comments is recorded in the task's activity history.
package comments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"kanban/internal/activity"
	"kanban/internal/apperr"
	"kanban/internal/boards"
	"kanban/internal/database"
	"kanban/internal/notifications"
	"kanban/internal/tasks"
)

// MaxLength is the maximum comment length in characters.
const MaxLength = 5000

// Comment is a comment joined with its author's name.
type Comment struct {
	ID         int64
	TaskID     int64
	AuthorID   int64
	AuthorName string
	Content    string
	CreatedAt  database.Time
	UpdatedAt  database.Time
}

// Edited reports whether the comment was changed after posting.
func (c Comment) Edited() bool { return c.UpdatedAt.After(c.CreatedAt.Time) }

// Service implements comment operations.
type Service struct {
	DB     *sql.DB
	Boards *boards.Service
	Tasks  *tasks.Service
}

func validate(content string) (string, error) {
	content = strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if content == "" {
		return "", apperr.Invalid("comment cannot be empty")
	}
	if utf8.RuneCountInString(content) > MaxLength {
		return "", apperr.Invalid("comment must be %d characters or fewer", MaxLength)
	}
	return content, nil
}

// excerpt shortens text for activity descriptions and notifications.
func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n])) + "…"
}

// ForTask lists a task's comments, oldest first, after checking access.
func (s *Service) ForTask(ctx context.Context, actorID, taskID int64) ([]Comment, error) {
	if _, _, err := s.Tasks.Get(ctx, actorID, taskID); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT c.id, c.task_id, c.author_id, u.name, c.content, c.created_at, c.updated_at
		FROM comments c JOIN users u ON u.id = c.author_id
		WHERE c.task_id = ? ORDER BY c.created_at, c.id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.AuthorID, &c.AuthorName, &c.Content, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Add posts a comment as actorID. The task's assignee and creator are
// notified (once each, and never about their own comment).
func (s *Service) Add(ctx context.Context, actorID, taskID int64, content string) (Comment, error) {
	content, err := validate(content)
	if err != nil {
		return Comment{}, err
	}
	var id int64
	err = database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		t, err := tasks.Load(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if _, err := boards.Require(ctx, tx, t.BoardID, actorID, boards.RoleMember); err != nil {
			return err
		}
		now := database.Now()
		res, err := tx.ExecContext(ctx,
			`INSERT INTO comments (task_id, author_id, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			taskID, actorID, content, now, now)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		if err := activity.Record(ctx, tx, activity.Entry{
			TaskID: t.ID, BoardID: t.BoardID, TaskTitle: t.Title, UserID: actorID,
			Action:      activity.CommentAdded,
			Description: fmt.Sprintf("commented: “%s”", excerpt(content, 200)),
			NewValue:    activity.Str(content),
		}); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET updated_at = ? WHERE id = ?`, now, t.ID); err != nil {
			return err
		}
		note := notifications.New{ActorID: actorID, TaskID: t.ID, BoardID: t.BoardID}
		if t.AssigneeID != 0 {
			note.RecipientID = t.AssigneeID
			note.Message = fmt.Sprintf("commented on “%s”, which is assigned to you: “%s”", t.Title, excerpt(content, 80))
			if err := notifications.Create(ctx, tx, note); err != nil {
				return err
			}
		}
		if t.CreatorID != t.AssigneeID {
			note.RecipientID = t.CreatorID
			note.Message = fmt.Sprintf("commented on your task “%s”: “%s”", t.Title, excerpt(content, 80))
			if err := notifications.Create(ctx, tx, note); err != nil {
				return err
			}
		}
		return boards.Touch(ctx, tx, t.BoardID)
	})
	if err != nil {
		return Comment{}, err
	}
	return s.get(ctx, s.DB, id)
}

func (s *Service) get(ctx context.Context, q database.DBTX, id int64) (Comment, error) {
	var c Comment
	err := q.QueryRowContext(ctx, `
		SELECT c.id, c.task_id, c.author_id, u.name, c.content, c.created_at, c.updated_at
		FROM comments c JOIN users u ON u.id = c.author_id WHERE c.id = ?`, id).
		Scan(&c.ID, &c.TaskID, &c.AuthorID, &c.AuthorName, &c.Content, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Comment{}, apperr.ErrNotFound
	}
	return c, err
}

// Get returns a comment if the actor can see its task.
func (s *Service) Get(ctx context.Context, actorID, id int64) (Comment, error) {
	c, err := s.get(ctx, s.DB, id)
	if err != nil {
		return Comment{}, err
	}
	if _, _, err := s.Tasks.Get(ctx, actorID, c.TaskID); err != nil {
		return Comment{}, err
	}
	return c, nil
}

// Update edits a comment. Only its author may edit it.
func (s *Service) Update(ctx context.Context, actorID, commentID int64, content string) (Comment, error) {
	content, err := validate(content)
	if err != nil {
		return Comment{}, err
	}
	err = database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		c, err := s.get(ctx, tx, commentID)
		if err != nil {
			return err
		}
		t, err := tasks.Load(ctx, tx, c.TaskID)
		if err != nil {
			return err
		}
		if _, err := boards.RoleOf(ctx, tx, t.BoardID, actorID); err != nil {
			return err
		}
		if c.AuthorID != actorID {
			return apperr.Invalid("you can only edit your own comments")
		}
		if c.Content == content {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE comments SET content = ?, updated_at = ? WHERE id = ?`,
			content, database.Now(), commentID); err != nil {
			return err
		}
		return activity.Record(ctx, tx, activity.Entry{
			TaskID: t.ID, BoardID: t.BoardID, TaskTitle: t.Title, UserID: actorID,
			Action:      activity.CommentEdited,
			Description: fmt.Sprintf("edited their comment: “%s”", excerpt(content, 200)),
			OldValue:    activity.Str(c.Content), NewValue: activity.Str(content),
		})
	})
	if err != nil {
		return Comment{}, err
	}
	return s.get(ctx, s.DB, commentID)
}

// Delete removes a comment. Authors may delete their own comments; board
// owners and admins may delete any comment (moderation).
func (s *Service) Delete(ctx context.Context, actorID, commentID int64) (taskID int64, err error) {
	err = database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		c, err := s.get(ctx, tx, commentID)
		if err != nil {
			return err
		}
		taskID = c.TaskID
		t, err := tasks.Load(ctx, tx, c.TaskID)
		if err != nil {
			return err
		}
		role, err := boards.RoleOf(ctx, tx, t.BoardID, actorID)
		if err != nil {
			return err
		}
		if c.AuthorID != actorID && !role.AtLeast(boards.RoleAdmin) {
			return apperr.ErrForbidden
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, commentID); err != nil {
			return err
		}
		desc := "deleted their comment"
		if c.AuthorID != actorID {
			desc = "deleted a comment by " + c.AuthorName
		}
		return activity.Record(ctx, tx, activity.Entry{
			TaskID: t.ID, BoardID: t.BoardID, TaskTitle: t.Title, UserID: actorID,
			Action: activity.CommentDeleted, Description: desc, OldValue: activity.Str(c.Content),
		})
	})
	return taskID, err
}
