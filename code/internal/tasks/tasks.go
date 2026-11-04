// Package tasks implements tasks: creating, editing, assigning, moving,
// deleting and listing them. Every change is written together with its
// activity-log entries (and any notifications) in a single transaction, so
// the history can never disagree with the data.
package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"kanban/internal/activity"
	"kanban/internal/apperr"
	"kanban/internal/boards"
	"kanban/internal/database"
	"kanban/internal/notifications"
)

// Priority of a task.
type Priority string

const (
	Low    Priority = "low"
	Medium Priority = "medium"
	High   Priority = "high"
	Urgent Priority = "urgent"
)

// Priorities lists every priority from lowest to highest.
var Priorities = []Priority{Low, Medium, High, Urgent}

// Label returns the display name, e.g. "High".
func (p Priority) Label() string {
	if p == "" {
		return ""
	}
	return strings.ToUpper(string(p[:1])) + string(p[1:])
}

func parsePriority(s string) (Priority, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return Medium, nil
	}
	for _, p := range Priorities {
		if string(p) == s {
			return p, nil
		}
	}
	return "", apperr.Invalid("priority must be low, medium, high or urgent")
}

// Task is a task joined with the names needed for display.
type Task struct {
	ID           int64
	BoardID      int64
	BoardName    string
	ColumnID     int64
	ColumnName   string
	ColumnIsDone bool
	Title        string
	Description  string
	Priority     Priority
	DueDate      string // YYYY-MM-DD or ""
	AssigneeID   int64  // 0 when unassigned
	AssigneeName string
	Position     int
	CreatorID    int64
	CreatorName  string
	CreatedAt    database.Time
	UpdatedAt    database.Time
	CompletedAt  database.Time // zero unless the task is in a done column
	CommentCount int
	Labels       []boards.Label
}

// Due returns the due date as a time (zero if none).
func (t Task) Due() time.Time {
	d, _ := time.Parse(database.DateLayout, t.DueDate)
	return d
}

// IsOverdue reports whether the task is past due and not completed.
func (t Task) IsOverdue() bool {
	return t.DueDate != "" && !t.ColumnIsDone && t.DueDate < database.Today()
}

// IsDueSoon reports whether an open task is due today or within two days.
func (t Task) IsDueSoon() bool {
	if t.DueDate == "" || t.ColumnIsDone || t.IsOverdue() {
		return false
	}
	limit := time.Now().UTC().AddDate(0, 0, 2).Format(database.DateLayout)
	return t.DueDate <= limit
}

// HasLabel reports whether the task carries the label.
func (t Task) HasLabel(id int64) bool {
	for _, l := range t.Labels {
		if l.ID == id {
			return true
		}
	}
	return false
}

// Service implements task operations.
type Service struct {
	DB     *sql.DB
	Boards *boards.Service
}

// Input is the editable part of a task, as submitted by a form.
type Input struct {
	ColumnID    int64 // only used on create
	Title       string
	Description string
	Priority    string
	DueDate     string
	AssigneeID  int64 // 0 = unassigned
	LabelIDs    []int64
}

type cleanInput struct {
	title, description string
	priority           Priority
	dueDate            sql.NullString
	assigneeID         sql.NullInt64
	labelIDs           []int64
}

// validate checks and normalizes user input. Assignee and labels are
// verified against the board so users cannot reference foreign records.
func validate(ctx context.Context, q database.DBTX, boardID int64, in Input) (cleanInput, error) {
	var c cleanInput
	c.title = strings.Join(strings.Fields(in.Title), " ")
	if c.title == "" {
		return c, apperr.Invalid("task title is required")
	}
	if utf8.RuneCountInString(c.title) > 200 {
		return c, apperr.Invalid("task title must be 200 characters or fewer")
	}
	c.description = strings.TrimSpace(strings.ReplaceAll(in.Description, "\r\n", "\n"))
	if utf8.RuneCountInString(c.description) > 10000 {
		return c, apperr.Invalid("task description must be 10,000 characters or fewer")
	}
	p, err := parsePriority(in.Priority)
	if err != nil {
		return c, err
	}
	c.priority = p
	if due := strings.TrimSpace(in.DueDate); due != "" {
		if _, err := time.Parse(database.DateLayout, due); err != nil {
			return c, apperr.Invalid("due date must be a valid date (YYYY-MM-DD)")
		}
		c.dueDate = sql.NullString{String: due, Valid: true}
	}
	if in.AssigneeID != 0 {
		ok, err := boards.IsMember(ctx, q, boardID, in.AssigneeID)
		if err != nil {
			return c, err
		}
		if !ok {
			return c, apperr.Invalid("tasks can only be assigned to members of this board")
		}
		c.assigneeID = sql.NullInt64{Int64: in.AssigneeID, Valid: true}
	}
	seen := map[int64]bool{}
	for _, id := range in.LabelIDs {
		if id == 0 || seen[id] {
			continue
		}
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM labels WHERE id = ? AND board_id = ?`, id, boardID).Scan(&n); err != nil {
			return c, err
		}
		if n == 0 {
			return c, apperr.Invalid("unknown label")
		}
		seen[id] = true
		c.labelIDs = append(c.labelIDs, id)
	}
	return c, nil
}

// ---- reading ------------------------------------------------------------

const selectTask = `
	SELECT t.id, t.board_id, b.name, t.column_id, c.name, c.is_done, t.title, t.description, t.priority,
	       COALESCE(t.due_date, ''), COALESCE(t.assignee_id, 0), COALESCE(a.name, ''), t.position,
	       t.creator_id, cr.name, t.created_at, t.updated_at, t.completed_at,
	       (SELECT COUNT(*) FROM comments cm WHERE cm.task_id = t.id)
	FROM tasks t
	JOIN boards b   ON b.id = t.board_id
	JOIN columns c  ON c.id = t.column_id
	JOIN users cr   ON cr.id = t.creator_id
	LEFT JOIN users a ON a.id = t.assignee_id`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.BoardID, &t.BoardName, &t.ColumnID, &t.ColumnName, &t.ColumnIsDone, &t.Title,
		&t.Description, &t.Priority, &t.DueDate, &t.AssigneeID, &t.AssigneeName, &t.Position,
		&t.CreatorID, &t.CreatorName, &t.CreatedAt, &t.UpdatedAt, &t.CompletedAt, &t.CommentCount)
	return t, err
}

func queryTasks(ctx context.Context, q database.DBTX, query string, args ...any) ([]Task, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// load returns a task without permission checks.
func load(ctx context.Context, q database.DBTX, taskID int64) (Task, error) {
	t, err := scanTask(q.QueryRowContext(ctx, selectTask+` WHERE t.id = ?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, apperr.ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	t.Labels, err = taskLabels(ctx, q, taskID)
	return t, err
}

func taskLabels(ctx context.Context, q database.DBTX, taskID int64) ([]boards.Label, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT l.id, l.board_id, l.name, l.color FROM task_labels tl JOIN labels l ON l.id = tl.label_id
		WHERE tl.task_id = ? ORDER BY l.name COLLATE NOCASE`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []boards.Label
	for rows.Next() {
		var l boards.Label
		if err := rows.Scan(&l.ID, &l.BoardID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// attachLabels loads labels for many tasks with a single query.
func attachLabels(ctx context.Context, q database.DBTX, ts []Task) error {
	if len(ts) == 0 {
		return nil
	}
	idx := map[int64]int{}
	args := make([]any, len(ts))
	for i, t := range ts {
		idx[t.ID] = i
		args[i] = t.ID
	}
	rows, err := q.QueryContext(ctx, `
		SELECT tl.task_id, l.id, l.board_id, l.name, l.color
		FROM task_labels tl JOIN labels l ON l.id = tl.label_id
		WHERE tl.task_id IN (?`+strings.Repeat(",?", len(ts)-1)+`)
		ORDER BY l.name COLLATE NOCASE`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var taskID int64
		var l boards.Label
		if err := rows.Scan(&taskID, &l.ID, &l.BoardID, &l.Name, &l.Color); err != nil {
			return err
		}
		i := idx[taskID]
		ts[i].Labels = append(ts[i].Labels, l)
	}
	return rows.Err()
}

// Get returns a task and the actor's role on its board.
func (s *Service) Get(ctx context.Context, actorID, taskID int64) (Task, boards.Role, error) {
	t, err := load(ctx, s.DB, taskID)
	if err != nil {
		return Task{}, "", err
	}
	role, err := boards.RoleOf(ctx, s.DB, t.BoardID, actorID)
	if err != nil {
		return Task{}, "", err // non-members get ErrNotFound
	}
	return t, role, nil
}

// Filter narrows and orders the tasks shown on a board.
type Filter struct {
	Query      string // title search
	ColumnID   int64
	Priority   string
	AssigneeID int64 // -1 = unassigned
	CreatorID  int64
	LabelID    int64
	Due        string // overdue | today | week | none
	Sort       string // position (default) | created | due | priority
}

// Active reports whether any filter or non-default sort is applied.
func (f Filter) Active() bool {
	return f.Query != "" || f.ColumnID != 0 || f.Priority != "" || f.AssigneeID != 0 ||
		f.CreatorID != 0 || f.LabelID != 0 || f.Due != "" || (f.Sort != "" && f.Sort != "position")
}

// ListBoard returns the board's tasks matching the filter.
func (s *Service) ListBoard(ctx context.Context, actorID, boardID int64, f Filter) ([]Task, error) {
	if _, err := boards.RoleOf(ctx, s.DB, boardID, actorID); err != nil {
		return nil, err
	}
	where := []string{"t.board_id = ?"}
	args := []any{boardID}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, `t.title LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(q)+"%")
	}
	if f.ColumnID != 0 {
		where = append(where, "t.column_id = ?")
		args = append(args, f.ColumnID)
	}
	if f.Priority != "" {
		where = append(where, "t.priority = ?")
		args = append(args, f.Priority)
	}
	switch {
	case f.AssigneeID == -1:
		where = append(where, "t.assignee_id IS NULL")
	case f.AssigneeID > 0:
		where = append(where, "t.assignee_id = ?")
		args = append(args, f.AssigneeID)
	}
	if f.CreatorID != 0 {
		where = append(where, "t.creator_id = ?")
		args = append(args, f.CreatorID)
	}
	if f.LabelID != 0 {
		where = append(where, "EXISTS (SELECT 1 FROM task_labels tl WHERE tl.task_id = t.id AND tl.label_id = ?)")
		args = append(args, f.LabelID)
	}
	today := database.Today()
	switch f.Due {
	case "overdue":
		where = append(where, "t.due_date < ? AND c.is_done = 0")
		args = append(args, today)
	case "today":
		where = append(where, "t.due_date = ?")
		args = append(args, today)
	case "week":
		where = append(where, "t.due_date >= ? AND t.due_date <= ?")
		args = append(args, today, time.Now().UTC().AddDate(0, 0, 7).Format(database.DateLayout))
	case "none":
		where = append(where, "t.due_date IS NULL")
	}
	order := "c.position, t.position, t.id"
	switch f.Sort {
	case "created":
		order = "t.created_at DESC, t.id DESC"
	case "due":
		order = "t.due_date IS NULL, t.due_date, t.id"
	case "priority":
		order = "CASE t.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END, t.id"
	}
	ts, err := queryTasks(ctx, s.DB, selectTask+" WHERE "+strings.Join(where, " AND ")+" ORDER BY "+order, args...)
	if err != nil {
		return nil, err
	}
	return ts, attachLabels(ctx, s.DB, ts)
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// AssignedTo returns open tasks assigned to the user across all their boards.
func (s *Service) AssignedTo(ctx context.Context, userID int64, limit int) ([]Task, error) {
	ts, err := queryTasks(ctx, s.DB, selectTask+`
		JOIN board_members m ON m.board_id = t.board_id AND m.user_id = ?
		WHERE t.assignee_id = ? AND c.is_done = 0
		ORDER BY t.due_date IS NULL, t.due_date,
		         CASE t.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END
		LIMIT ?`, userID, userID, limit)
	if err != nil {
		return nil, err
	}
	return ts, attachLabels(ctx, s.DB, ts)
}

// RecentlyUpdated returns the most recently changed tasks on the user's boards.
func (s *Service) RecentlyUpdated(ctx context.Context, userID int64, limit int) ([]Task, error) {
	ts, err := queryTasks(ctx, s.DB, selectTask+`
		JOIN board_members m ON m.board_id = t.board_id AND m.user_id = ?
		ORDER BY t.updated_at DESC, t.id DESC
		LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	return ts, attachLabels(ctx, s.DB, ts)
}

// ---- writing ------------------------------------------------------------

// Create adds a task at the bottom of a column. The creator is always the
// authenticated actor and is never changed afterwards.
func (s *Service) Create(ctx context.Context, actorID, boardID int64, in Input) (Task, error) {
	var id int64
	err := database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		if _, err := boards.Require(ctx, tx, boardID, actorID, boards.RoleMember); err != nil {
			return err
		}
		col, err := boards.GetColumn(ctx, tx, boardID, in.ColumnID)
		if errors.Is(err, apperr.ErrNotFound) {
			return apperr.Invalid("please choose a column for the task")
		} else if err != nil {
			return err
		}
		c, err := validate(ctx, tx, boardID, in)
		if err != nil {
			return err
		}
		var pos int
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(position) + 1, 0) FROM tasks WHERE column_id = ?`, col.ID).Scan(&pos); err != nil {
			return err
		}
		now := database.Now()
		var completedAt sql.NullString
		if col.IsDone {
			completedAt = sql.NullString{String: now, Valid: true}
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO tasks (board_id, column_id, title, description, priority, due_date, assignee_id,
			                   position, creator_id, created_at, updated_at, completed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			boardID, col.ID, c.title, c.description, c.priority, c.dueDate, c.assigneeID,
			pos, actorID, now, now, completedAt)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		if err := setLabels(ctx, tx, id, c.labelIDs); err != nil {
			return err
		}
		log := logger{ctx: ctx, tx: tx, taskID: id, boardID: boardID, title: c.title, actorID: actorID}
		if err := log.add(activity.TaskCreated, fmt.Sprintf("created this task in %s", col.Name), nil, activity.Str(c.title)); err != nil {
			return err
		}
		if c.assigneeID.Valid {
			name, err := userName(ctx, tx, c.assigneeID.Int64)
			if err != nil {
				return err
			}
			if err := log.add(activity.Assigned, "assigned this task to "+name, nil, activity.Str(name)); err != nil {
				return err
			}
			if err := log.notify(c.assigneeID.Int64, fmt.Sprintf("assigned you to “%s”", c.title)); err != nil {
				return err
			}
		}
		return boards.Touch(ctx, tx, boardID)
	})
	if err != nil {
		return Task{}, err
	}
	return load(ctx, s.DB, id)
}

// Update edits a task's fields. Each changed field produces its own activity
// entry describing the old and new value. Moving between columns is done
// with Move, not Update.
func (s *Service) Update(ctx context.Context, actorID, taskID int64, in Input) (Task, error) {
	err := database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		old, err := load(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if _, err := boards.Require(ctx, tx, old.BoardID, actorID, boards.RoleMember); err != nil {
			return err
		}
		c, err := validate(ctx, tx, old.BoardID, in)
		if err != nil {
			return err
		}
		log := logger{ctx: ctx, tx: tx, taskID: taskID, boardID: old.BoardID, title: c.title, actorID: actorID}
		changed := false
		var assigneeNotified int64

		if c.title != old.Title {
			changed = true
			if err := log.add(activity.TitleChanged, fmt.Sprintf("renamed this task from “%s” to “%s”", old.Title, c.title),
				activity.Str(old.Title), activity.Str(c.title)); err != nil {
				return err
			}
		}
		if c.description != old.Description {
			changed = true
			desc := "updated the description"
			if old.Description == "" {
				desc = "added a description"
			} else if c.description == "" {
				desc = "removed the description"
			}
			if err := log.add(activity.DescriptionChanged, desc, activity.Str(old.Description), activity.Str(c.description)); err != nil {
				return err
			}
		}
		if c.priority != old.Priority {
			changed = true
			if err := log.add(activity.PriorityChanged,
				fmt.Sprintf("changed priority from %s to %s", old.Priority.Label(), c.priority.Label()),
				activity.Str(string(old.Priority)), activity.Str(string(c.priority))); err != nil {
				return err
			}
		}
		if c.dueDate.String != old.DueDate {
			changed = true
			var desc string
			switch {
			case old.DueDate == "":
				desc = "set the due date to " + FormatDate(c.dueDate.String)
			case !c.dueDate.Valid:
				desc = "removed the due date (was " + FormatDate(old.DueDate) + ")"
			default:
				desc = fmt.Sprintf("changed the due date from %s to %s", FormatDate(old.DueDate), FormatDate(c.dueDate.String))
			}
			if err := log.add(activity.DueDateChanged, desc, optional(old.DueDate), optional(c.dueDate.String)); err != nil {
				return err
			}
		}
		if c.assigneeID.Int64 != old.AssigneeID {
			changed = true
			newName := ""
			if c.assigneeID.Valid {
				if newName, err = userName(ctx, tx, c.assigneeID.Int64); err != nil {
					return err
				}
			}
			switch {
			case old.AssigneeID == 0:
				err = log.add(activity.Assigned, "assigned this task to "+newName, nil, activity.Str(newName))
			case !c.assigneeID.Valid:
				err = log.add(activity.Unassigned, "unassigned "+old.AssigneeName, activity.Str(old.AssigneeName), nil)
			default:
				err = log.add(activity.Reassigned,
					fmt.Sprintf("reassigned this task from %s to %s", old.AssigneeName, newName),
					activity.Str(old.AssigneeName), activity.Str(newName))
			}
			if err != nil {
				return err
			}
			if c.assigneeID.Valid {
				assigneeNotified = c.assigneeID.Int64
				if err := log.notify(assigneeNotified, fmt.Sprintf("assigned you to “%s”", c.title)); err != nil {
					return err
				}
			}
		}
		if added, removed := diffLabels(old.Labels, c.labelIDs); len(added)+len(removed) > 0 {
			changed = true
			names, err := labelNames(ctx, tx, added)
			if err != nil {
				return err
			}
			var parts []string
			if len(added) > 0 {
				parts = append(parts, "added "+labelList(names))
			}
			if len(removed) > 0 {
				parts = append(parts, "removed "+labelList(removed))
			}
			oldNames := make([]string, len(old.Labels))
			for i, l := range old.Labels {
				oldNames[i] = l.Name
			}
			newNames, err := labelNames(ctx, tx, c.labelIDs)
			if err != nil {
				return err
			}
			if err := log.add(activity.LabelsChanged, strings.Join(parts, " and "),
				activity.Str(strings.Join(oldNames, ", ")), activity.Str(strings.Join(newNames, ", "))); err != nil {
				return err
			}
			if err := setLabels(ctx, tx, taskID, c.labelIDs); err != nil {
				return err
			}
		}
		if !changed {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks SET title = ?, description = ?, priority = ?, due_date = ?, assignee_id = ?, updated_at = ?
			WHERE id = ?`,
			c.title, c.description, c.priority, c.dueDate, c.assigneeID, database.Now(), taskID); err != nil {
			return err
		}
		if old.CreatorID != assigneeNotified {
			if err := log.notify(old.CreatorID, fmt.Sprintf("updated your task “%s”", c.title)); err != nil {
				return err
			}
		}
		return boards.Touch(ctx, tx, old.BoardID)
	})
	if err != nil {
		return Task{}, err
	}
	return load(ctx, s.DB, taskID)
}

// Move places a task in a column at a zero-based position. Positions in
// both affected columns stay contiguous. Moving into a "done" column marks
// the task completed; moving out of one reopens it.
func (s *Service) Move(ctx context.Context, actorID, taskID, toColumnID int64, toPos int) (Task, error) {
	err := database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		t, err := load(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if _, err := boards.Require(ctx, tx, t.BoardID, actorID, boards.RoleMember); err != nil {
			return err
		}
		to, err := boards.GetColumn(ctx, tx, t.BoardID, toColumnID)
		if errors.Is(err, apperr.ErrNotFound) {
			return apperr.Invalid("that column does not exist on this board")
		} else if err != nil {
			return err
		}
		from := boards.Column{ID: t.ColumnID, Name: t.ColumnName, IsDone: t.ColumnIsDone}
		return moveTask(ctx, tx, actorID, t, from, to, toPos, "")
	})
	if err != nil {
		return Task{}, err
	}
	return load(ctx, s.DB, taskID)
}

// moveTask does the position bookkeeping and auditing for a move. `reason`
// is appended to the activity description when non-empty.
func moveTask(ctx context.Context, tx *sql.Tx, actorID int64, t Task, from, to boards.Column, toPos int, reason string) error {
	log := logger{ctx: ctx, tx: tx, taskID: t.ID, boardID: t.BoardID, title: t.Title, actorID: actorID}
	now := database.Now()
	if toPos < 0 {
		toPos = 0
	}

	if from.ID == to.ID {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE column_id = ?`, to.ID).Scan(&n); err != nil {
			return err
		}
		if toPos > n-1 {
			toPos = n - 1
		}
		if toPos == t.Position {
			return nil // nothing changed, nothing to record
		}
		if toPos < t.Position {
			_, err := tx.ExecContext(ctx, `UPDATE tasks SET position = position + 1
				WHERE column_id = ? AND position >= ? AND position < ?`, to.ID, toPos, t.Position)
			if err != nil {
				return err
			}
		} else {
			_, err := tx.ExecContext(ctx, `UPDATE tasks SET position = position - 1
				WHERE column_id = ? AND position > ? AND position <= ?`, to.ID, t.Position, toPos)
			if err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET position = ?, updated_at = ? WHERE id = ?`, toPos, now, t.ID); err != nil {
			return err
		}
		return log.add(activity.Reordered,
			fmt.Sprintf("reordered this task within %s (position %d → %d)", to.Name, t.Position+1, toPos+1),
			activity.Str(fmt.Sprint(t.Position+1)), activity.Str(fmt.Sprint(toPos+1)))
	}

	// Close the gap in the old column and open one in the new column.
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET position = position - 1 WHERE column_id = ? AND position > ?`,
		from.ID, t.Position); err != nil {
		return err
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE column_id = ?`, to.ID).Scan(&n); err != nil {
		return err
	}
	if toPos > n {
		toPos = n
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET position = position + 1 WHERE column_id = ? AND position >= ?`,
		to.ID, toPos); err != nil {
		return err
	}
	completedAt := sql.NullString{}
	if to.IsDone {
		completedAt = sql.NullString{String: now, Valid: true}
		if from.IsDone && !t.CompletedAt.IsZero() {
			completedAt.String = t.CompletedAt.UTC().Format(database.TimeLayout)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET column_id = ?, position = ?, updated_at = ?, completed_at = ? WHERE id = ?`,
		to.ID, toPos, now, completedAt, t.ID); err != nil {
		return err
	}

	action, desc := activity.Moved, fmt.Sprintf("moved this task from %s to %s", from.Name, to.Name)
	note := fmt.Sprintf("moved your task “%s” to %s", t.Title, to.Name)
	switch {
	case to.IsDone && !from.IsDone:
		action, desc = activity.Completed, fmt.Sprintf("completed this task (moved from %s to %s)", from.Name, to.Name)
		note = fmt.Sprintf("completed your task “%s”", t.Title)
	case from.IsDone && !to.IsDone:
		action, desc = activity.Reopened, fmt.Sprintf("reopened this task (moved from %s to %s)", from.Name, to.Name)
		note = fmt.Sprintf("reopened your task “%s”", t.Title)
	}
	if reason != "" {
		desc += " " + reason
	}
	if err := log.add(action, desc, activity.Str(from.Name), activity.Str(to.Name)); err != nil {
		return err
	}
	if err := log.notify(t.CreatorID, note); err != nil {
		return err
	}
	return boards.Touch(ctx, tx, t.BoardID)
}

// Delete removes a task. Only its creator or a board admin/owner may do so.
// A "task deleted" entry is kept in the board's activity log.
func (s *Service) Delete(ctx context.Context, actorID, taskID int64) error {
	return database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		t, err := load(ctx, tx, taskID)
		if err != nil {
			return err
		}
		role, err := boards.RoleOf(ctx, tx, t.BoardID, actorID)
		if err != nil {
			return err
		}
		if t.CreatorID != actorID && !role.AtLeast(boards.RoleAdmin) {
			return apperr.ErrForbidden
		}
		log := logger{ctx: ctx, tx: tx, taskID: t.ID, boardID: t.BoardID, title: t.Title, actorID: actorID}
		if err := log.add(activity.TaskDeleted, fmt.Sprintf("deleted the task “%s” from %s", t.Title, t.ColumnName),
			activity.Str(t.Title), nil); err != nil {
			return err
		}
		if err := log.notify(t.CreatorID, fmt.Sprintf("deleted your task “%s”", t.Title)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, t.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET position = position - 1 WHERE column_id = ? AND position > ?`,
			t.ColumnID, t.Position); err != nil {
			return err
		}
		return boards.Touch(ctx, tx, t.BoardID)
	})
}

// DeleteColumn deletes a column (admin or owner). If it still holds tasks,
// they are first moved to the bottom of moveToID, and each move is recorded
// in the task's history. A board must keep at least one column.
func (s *Service) DeleteColumn(ctx context.Context, actorID, boardID, columnID, moveToID int64) error {
	return database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		if _, err := boards.Require(ctx, tx, boardID, actorID, boards.RoleAdmin); err != nil {
			return err
		}
		col, err := boards.GetColumn(ctx, tx, boardID, columnID)
		if err != nil {
			return err
		}
		var colCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM columns WHERE board_id = ?`, boardID).Scan(&colCount); err != nil {
			return err
		}
		if colCount <= 1 {
			return apperr.Invalid("a board needs at least one column")
		}
		ts, err := queryTasks(ctx, tx, selectTask+` WHERE t.column_id = ? ORDER BY t.position`, columnID)
		if err != nil {
			return err
		}
		if len(ts) > 0 {
			if moveToID == 0 {
				return apperr.Invalid("“%s” still has %s; choose a column to move them to", col.Name, plural(len(ts), "task"))
			}
			if moveToID == columnID {
				return apperr.Invalid("choose a different column to move the tasks to")
			}
			to, err := boards.GetColumn(ctx, tx, boardID, moveToID)
			if errors.Is(err, apperr.ErrNotFound) {
				return apperr.Invalid("the destination column does not exist on this board")
			} else if err != nil {
				return err
			}
			reason := fmt.Sprintf("because the column “%s” was deleted", col.Name)
			for _, t := range ts {
				// Each earlier move shifted the remaining tasks up, so the
				// next task is always at position 0.
				t.Position = 0
				if err := moveTask(ctx, tx, actorID, t, col, to, 1<<30, reason); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM columns WHERE id = ?`, columnID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE columns SET position = position - 1 WHERE board_id = ? AND position > ?`,
			boardID, col.Position); err != nil {
			return err
		}
		return boards.Touch(ctx, tx, boardID)
	})
}

// ---- helpers ------------------------------------------------------------

// logger records activity and notifications for one task within a tx.
type logger struct {
	ctx     context.Context
	tx      *sql.Tx
	taskID  int64
	boardID int64
	title   string
	actorID int64
}

func (l logger) add(action, description string, oldV, newV *string) error {
	return activity.Record(l.ctx, l.tx, activity.Entry{
		TaskID: l.taskID, BoardID: l.boardID, TaskTitle: l.title, UserID: l.actorID,
		Action: action, Description: description, OldValue: oldV, NewValue: newV,
	})
}

func (l logger) notify(recipient int64, message string) error {
	return notifications.Create(l.ctx, l.tx, notifications.New{
		RecipientID: recipient, ActorID: l.actorID, TaskID: l.taskID, BoardID: l.boardID, Message: message,
	})
}

func userName(ctx context.Context, q database.DBTX, id int64) (string, error) {
	var name string
	err := q.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, id).Scan(&name)
	return name, err
}

func setLabels(ctx context.Context, tx *sql.Tx, taskID int64, ids []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_labels WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_labels (task_id, label_id) VALUES (?, ?)`, taskID, id); err != nil {
			return err
		}
	}
	return nil
}

func labelNames(ctx context.Context, q database.DBTX, ids []int64) ([]string, error) {
	var out []string
	for _, id := range ids {
		var n string
		if err := q.QueryRowContext(ctx, `SELECT name FROM labels WHERE id = ?`, id).Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// diffLabels returns the IDs being added and the names being removed.
func diffLabels(old []boards.Label, next []int64) (added []int64, removed []string) {
	oldSet := map[int64]bool{}
	for _, l := range old {
		oldSet[l.ID] = true
	}
	nextSet := map[int64]bool{}
	for _, id := range next {
		nextSet[id] = true
		if !oldSet[id] {
			added = append(added, id)
		}
	}
	for _, l := range old {
		if !nextSet[l.ID] {
			removed = append(removed, l.Name)
		}
	}
	return added, removed
}

// labelList renders names as: the label “bug” / the labels “bug”, “ui”.
func labelList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = "“" + n + "”"
	}
	if len(q) == 1 {
		return "the label " + q[0]
	}
	return "the labels " + strings.Join(q, ", ")
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// FormatDate turns YYYY-MM-DD into "Nov 4, 2026".
func FormatDate(d string) string {
	t, err := time.Parse(database.DateLayout, d)
	if err != nil {
		return d
	}
	return t.Format("Jan 2, 2006")
}
