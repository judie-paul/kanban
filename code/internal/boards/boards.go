// Package boards manages boards, board membership and roles, columns and
// labels. Every method that acts on behalf of a user checks that user's role
// on the board, so permissions are enforced on the server no matter what the
// browser sends.
package boards

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"kanban/internal/activity"
	"kanban/internal/apperr"
	"kanban/internal/database"
	"kanban/internal/notifications"
	"kanban/internal/users"
)

// Role is a user's role on a board.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

func (r Role) rank() int {
	switch r {
	case RoleOwner:
		return 3
	case RoleAdmin:
		return 2
	case RoleMember:
		return 1
	}
	return 0
}

// AtLeast reports whether r is at least as powerful as min.
func (r Role) AtLeast(min Role) bool { return r.rank() >= min.rank() }

// Label returns a human readable role name.
func (r Role) Label() string {
	switch r {
	case RoleOwner:
		return "Owner"
	case RoleAdmin:
		return "Admin"
	case RoleMember:
		return "Member"
	}
	return string(r)
}

// DefaultColumns are created for every new board. The last one is the
// "done" column: tasks moved there count as completed.
var DefaultColumns = []string{"Backlog", "To Do", "In Progress", "Review", "Done"}

// Board is a project containing columns and tasks.
type Board struct {
	ID          int64
	Name        string
	Description string
	OwnerID     int64
	OwnerName   string
	CreatedAt   database.Time
	UpdatedAt   database.Time
}

// Summary is a board plus the counts shown on the dashboard.
type Summary struct {
	Board
	Role           Role
	TaskCount      int
	CompletedCount int
	OverdueCount   int
	MemberCount    int
}

// Member is a user's membership on a board.
type Member struct {
	UserID  int64
	Name    string
	Email   string
	Role    Role
	AddedAt database.Time
}

// Column is a Kanban column.
type Column struct {
	ID       int64
	BoardID  int64
	Name     string
	Position int
	IsDone   bool
}

// Label is a coloured tag that can be attached to tasks.
type Label struct {
	ID      int64
	BoardID int64
	Name    string
	Color   string
}

// LabelColors is the fixed palette of label colours (CSS class suffixes).
var LabelColors = []string{"gray", "red", "orange", "yellow", "green", "teal", "blue", "purple", "pink"}

// Service implements board operations.
type Service struct {
	DB *sql.DB
}

// ---- access control -----------------------------------------------------

// RoleOf returns the user's role on the board, or ErrNotFound if the user is
// not a member (we do not reveal whether the board exists).
func RoleOf(ctx context.Context, q database.DBTX, boardID, userID int64) (Role, error) {
	var r Role
	err := q.QueryRowContext(ctx,
		`SELECT role FROM board_members WHERE board_id = ? AND user_id = ?`, boardID, userID).Scan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return "", apperr.ErrNotFound
	}
	return r, err
}

// Require checks that the user is a member with at least the given role.
func Require(ctx context.Context, q database.DBTX, boardID, userID int64, min Role) (Role, error) {
	r, err := RoleOf(ctx, q, boardID, userID)
	if err != nil {
		return "", err
	}
	if !r.AtLeast(min) {
		return r, apperr.ErrForbidden
	}
	return r, nil
}

// IsMember reports whether userID belongs to the board.
func IsMember(ctx context.Context, q database.DBTX, boardID, userID int64) (bool, error) {
	_, err := RoleOf(ctx, q, boardID, userID)
	if errors.Is(err, apperr.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// ---- boards -------------------------------------------------------------

func validateBoard(name, description string) (string, string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return "", "", apperr.Invalid("board name is required")
	}
	if utf8.RuneCountInString(name) > 100 {
		return "", "", apperr.Invalid("board name must be 100 characters or fewer")
	}
	if utf8.RuneCountInString(description) > 2000 {
		return "", "", apperr.Invalid("board description must be 2000 characters or fewer")
	}
	return name, description, nil
}

// Create makes a new board owned by actorID, with the default columns.
func (s *Service) Create(ctx context.Context, actorID int64, name, description string) (Board, error) {
	name, description, err := validateBoard(name, description)
	if err != nil {
		return Board{}, err
	}
	var id int64
	err = database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		now := database.Now()
		res, err := tx.ExecContext(ctx,
			`INSERT INTO boards (name, description, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			name, description, actorID, now, now)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO board_members (board_id, user_id, role, added_at) VALUES (?, ?, 'owner', ?)`,
			id, actorID, now); err != nil {
			return err
		}
		for i, col := range DefaultColumns {
			isDone := i == len(DefaultColumns)-1
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO columns (board_id, name, position, is_done, created_at) VALUES (?, ?, ?, ?, ?)`,
				id, col, i, isDone, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Board{}, err
	}
	b, _, err := s.Get(ctx, actorID, id)
	return b, err
}

// Get returns a board and the user's role on it.
func (s *Service) Get(ctx context.Context, actorID, boardID int64) (Board, Role, error) {
	role, err := RoleOf(ctx, s.DB, boardID, actorID)
	if err != nil {
		return Board{}, "", err
	}
	var b Board
	err = s.DB.QueryRowContext(ctx, `
		SELECT b.id, b.name, b.description, b.owner_id, u.name, b.created_at, b.updated_at
		FROM boards b JOIN users u ON u.id = b.owner_id
		WHERE b.id = ?`, boardID).
		Scan(&b.ID, &b.Name, &b.Description, &b.OwnerID, &b.OwnerName, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Board{}, "", apperr.ErrNotFound
	}
	return b, role, err
}

// ListForUser returns every board the user belongs to with dashboard counts.
func (s *Service) ListForUser(ctx context.Context, userID int64) ([]Summary, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT b.id, b.name, b.description, b.owner_id, u.name, b.created_at, b.updated_at, m.role,
		       (SELECT COUNT(*) FROM tasks t WHERE t.board_id = b.id),
		       (SELECT COUNT(*) FROM tasks t JOIN columns c ON c.id = t.column_id
		         WHERE t.board_id = b.id AND c.is_done = 1),
		       (SELECT COUNT(*) FROM tasks t JOIN columns c ON c.id = t.column_id
		         WHERE t.board_id = b.id AND c.is_done = 0 AND t.due_date IS NOT NULL AND t.due_date < ?),
		       (SELECT COUNT(*) FROM board_members bm WHERE bm.board_id = b.id)
		FROM boards b
		JOIN board_members m ON m.board_id = b.id AND m.user_id = ?
		JOIN users u ON u.id = b.owner_id
		ORDER BY b.updated_at DESC, b.id DESC`, database.Today(), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var s Summary
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.OwnerID, &s.OwnerName, &s.CreatedAt, &s.UpdatedAt,
			&s.Role, &s.TaskCount, &s.CompletedCount, &s.OverdueCount, &s.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Update changes the board's name and description (admin or owner).
func (s *Service) Update(ctx context.Context, actorID, boardID int64, name, description string) error {
	if _, err := Require(ctx, s.DB, boardID, actorID, RoleAdmin); err != nil {
		return err
	}
	name, description, err := validateBoard(name, description)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE boards SET name = ?, description = ?, updated_at = ? WHERE id = ?`,
		name, description, database.Now(), boardID)
	return err
}

// Delete removes a board and everything on it (owner only).
func (s *Service) Delete(ctx context.Context, actorID, boardID int64) error {
	if _, err := Require(ctx, s.DB, boardID, actorID, RoleOwner); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM boards WHERE id = ?`, boardID)
	return err
}

// Touch bumps a board's updated_at so it sorts first on the dashboard.
func Touch(ctx context.Context, q database.DBTX, boardID int64) error {
	_, err := q.ExecContext(ctx, `UPDATE boards SET updated_at = ? WHERE id = ?`, database.Now(), boardID)
	return err
}

// ---- membership ---------------------------------------------------------

// Members lists the board's members, owner first.
func (s *Service) Members(ctx context.Context, actorID, boardID int64) ([]Member, error) {
	if _, err := RoleOf(ctx, s.DB, boardID, actorID); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT u.id, u.name, u.email, m.role, m.added_at
		FROM board_members m JOIN users u ON u.id = m.user_id
		WHERE m.board_id = ?
		ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, u.name`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Name, &m.Email, &m.Role, &m.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func parseAssignableRole(role string) (Role, error) {
	switch Role(role) {
	case RoleAdmin, RoleMember:
		return Role(role), nil
	}
	return "", apperr.Invalid("role must be admin or member")
}

// AddMember adds a registered user (by email) to the board (owner only).
func (s *Service) AddMember(ctx context.Context, actorID, boardID int64, email, role string) (Member, error) {
	if _, err := Require(ctx, s.DB, boardID, actorID, RoleOwner); err != nil {
		return Member{}, err
	}
	r, err := parseAssignableRole(role)
	if err != nil {
		return Member{}, err
	}
	var u users.User
	err = s.DB.QueryRowContext(ctx, `SELECT id, name, email FROM users WHERE email = ?`,
		users.NormalizeEmail(email)).Scan(&u.ID, &u.Name, &u.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, apperr.Invalid("no registered user has the email %q — ask them to sign up first", strings.TrimSpace(email))
	}
	if err != nil {
		return Member{}, err
	}
	var boardName string
	err = database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		if ok, err := IsMember(ctx, tx, boardID, u.ID); err != nil {
			return err
		} else if ok {
			return apperr.Invalid("%s is already a member of this board", u.Name)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO board_members (board_id, user_id, role, added_at) VALUES (?, ?, ?, ?)`,
			boardID, u.ID, r, database.Now()); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT name FROM boards WHERE id = ?`, boardID).Scan(&boardName); err != nil {
			return err
		}
		return notifications.Create(ctx, tx, notifications.New{
			RecipientID: u.ID, ActorID: actorID, BoardID: boardID,
			Message: fmt.Sprintf("added you to the board “%s” as %s", boardName, r.Label()),
		})
	})
	if err != nil {
		return Member{}, err
	}
	return Member{UserID: u.ID, Name: u.Name, Email: u.Email, Role: r}, nil
}

// ChangeRole switches a member between admin and member (owner only).
func (s *Service) ChangeRole(ctx context.Context, actorID, boardID, userID int64, role string) error {
	if _, err := Require(ctx, s.DB, boardID, actorID, RoleOwner); err != nil {
		return err
	}
	r, err := parseAssignableRole(role)
	if err != nil {
		return err
	}
	current, err := RoleOf(ctx, s.DB, boardID, userID)
	if err != nil {
		return err
	}
	if current == RoleOwner {
		return apperr.Invalid("the owner's role cannot be changed")
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE board_members SET role = ? WHERE board_id = ? AND user_id = ?`, r, boardID, userID)
	return err
}

// RemoveMember removes a member (owner only). Tasks assigned to them are
// unassigned, and that is recorded in each task's history.
func (s *Service) RemoveMember(ctx context.Context, actorID, boardID, userID int64) error {
	if _, err := Require(ctx, s.DB, boardID, actorID, RoleOwner); err != nil {
		return err
	}
	return s.removeMember(ctx, actorID, boardID, userID, "was removed from the board")
}

// Leave lets a non-owner remove themselves from a board.
func (s *Service) Leave(ctx context.Context, actorID, boardID int64) error {
	r, err := RoleOf(ctx, s.DB, boardID, actorID)
	if err != nil {
		return err
	}
	if r == RoleOwner {
		return apperr.Invalid("the owner cannot leave their own board; delete it instead")
	}
	return s.removeMember(ctx, actorID, boardID, actorID, "left the board")
}

func (s *Service) removeMember(ctx context.Context, actorID, boardID, userID int64, reason string) error {
	return database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		r, err := RoleOf(ctx, tx, boardID, userID)
		if err != nil {
			return err
		}
		if r == RoleOwner {
			return apperr.Invalid("the board owner cannot be removed")
		}
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, userID).Scan(&name); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT id, title FROM tasks WHERE board_id = ? AND assignee_id = ?`, boardID, userID)
		if err != nil {
			return err
		}
		type t struct {
			id    int64
			title string
		}
		var assigned []t
		for rows.Next() {
			var x t
			if err := rows.Scan(&x.id, &x.title); err != nil {
				rows.Close()
				return err
			}
			assigned = append(assigned, x)
		}
		rows.Close()
		now := database.Now()
		for _, x := range assigned {
			if _, err := tx.ExecContext(ctx,
				`UPDATE tasks SET assignee_id = NULL, updated_at = ? WHERE id = ?`, now, x.id); err != nil {
				return err
			}
			if err := activity.Record(ctx, tx, activity.Entry{
				TaskID: x.id, BoardID: boardID, TaskTitle: x.title, UserID: actorID,
				Action:      activity.Unassigned,
				Description: fmt.Sprintf("unassigned %s because they %s", name, reason),
				OldValue:    activity.Str(name),
			}); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM board_members WHERE board_id = ? AND user_id = ?`, boardID, userID)
		return err
	})
}

// ---- columns ------------------------------------------------------------

// Columns returns the board's columns ordered by position. Callers must have
// checked access.
func Columns(ctx context.Context, q database.DBTX, boardID int64) ([]Column, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, board_id, name, position, is_done FROM columns WHERE board_id = ? ORDER BY position, id`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.ID, &c.BoardID, &c.Name, &c.Position, &c.IsDone); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetColumn returns one column, verifying it belongs to the board.
func GetColumn(ctx context.Context, q database.DBTX, boardID, columnID int64) (Column, error) {
	var c Column
	err := q.QueryRowContext(ctx,
		`SELECT id, board_id, name, position, is_done FROM columns WHERE id = ? AND board_id = ?`, columnID, boardID).
		Scan(&c.ID, &c.BoardID, &c.Name, &c.Position, &c.IsDone)
	if errors.Is(err, sql.ErrNoRows) {
		return Column{}, apperr.ErrNotFound
	}
	return c, err
}

// Columns returns the board's columns for a member.
func (s *Service) Columns(ctx context.Context, actorID, boardID int64) ([]Column, error) {
	if _, err := RoleOf(ctx, s.DB, boardID, actorID); err != nil {
		return nil, err
	}
	return Columns(ctx, s.DB, boardID)
}

func validateColumnName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", apperr.Invalid("column name is required")
	}
	if utf8.RuneCountInString(name) > 50 {
		return "", apperr.Invalid("column name must be 50 characters or fewer")
	}
	return name, nil
}

// CreateColumn appends a column to the board (admin or owner).
func (s *Service) CreateColumn(ctx context.Context, actorID, boardID int64, name string, isDone bool) (Column, error) {
	if _, err := Require(ctx, s.DB, boardID, actorID, RoleAdmin); err != nil {
		return Column{}, err
	}
	name, err := validateColumnName(name)
	if err != nil {
		return Column{}, err
	}
	var c Column
	err = database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		var pos int
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(position) + 1, 0) FROM columns WHERE board_id = ?`, boardID).Scan(&pos); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO columns (board_id, name, position, is_done, created_at) VALUES (?, ?, ?, ?, ?)`,
			boardID, name, pos, isDone, database.Now())
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		c = Column{ID: id, BoardID: boardID, Name: name, Position: pos, IsDone: isDone}
		return Touch(ctx, tx, boardID)
	})
	return c, err
}

// UpdateColumn renames a column and sets whether it is a "done" column.
func (s *Service) UpdateColumn(ctx context.Context, actorID, boardID, columnID int64, name string, isDone bool) error {
	if _, err := Require(ctx, s.DB, boardID, actorID, RoleAdmin); err != nil {
		return err
	}
	name, err := validateColumnName(name)
	if err != nil {
		return err
	}
	if _, err := GetColumn(ctx, s.DB, boardID, columnID); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE columns SET name = ?, is_done = ? WHERE id = ? AND board_id = ?`, name, isDone, columnID, boardID)
	return err
}

// ReorderColumn moves a column to a new zero-based position, shifting the
// others. Positions stay contiguous (0..n-1).
func (s *Service) ReorderColumn(ctx context.Context, actorID, boardID, columnID int64, newPos int) error {
	if _, err := Require(ctx, s.DB, boardID, actorID, RoleAdmin); err != nil {
		return err
	}
	return database.WithTx(ctx, s.DB, func(tx *sql.Tx) error {
		cols, err := Columns(ctx, tx, boardID)
		if err != nil {
			return err
		}
		from := -1
		for i, c := range cols {
			if c.ID == columnID {
				from = i
			}
		}
		if from < 0 {
			return apperr.ErrNotFound
		}
		if newPos < 0 {
			newPos = 0
		}
		if newPos > len(cols)-1 {
			newPos = len(cols) - 1
		}
		moved := cols[from]
		cols = append(cols[:from], cols[from+1:]...)
		cols = append(cols[:newPos], append([]Column{moved}, cols[newPos:]...)...)
		for i, c := range cols {
			if _, err := tx.ExecContext(ctx, `UPDATE columns SET position = ? WHERE id = ?`, i, c.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---- labels -------------------------------------------------------------

// Labels returns the board's labels. Callers must have checked access.
func Labels(ctx context.Context, q database.DBTX, boardID int64) ([]Label, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, board_id, name, color FROM labels WHERE board_id = ? ORDER BY name COLLATE NOCASE`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Label
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.BoardID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Labels returns the board's labels for a member.
func (s *Service) Labels(ctx context.Context, actorID, boardID int64) ([]Label, error) {
	if _, err := RoleOf(ctx, s.DB, boardID, actorID); err != nil {
		return nil, err
	}
	return Labels(ctx, s.DB, boardID)
}

// CreateLabel adds a label to the board (any member).
func (s *Service) CreateLabel(ctx context.Context, actorID, boardID int64, name, color string) (Label, error) {
	if _, err := Require(ctx, s.DB, boardID, actorID, RoleMember); err != nil {
		return Label{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 30 {
		return Label{}, apperr.Invalid("label name must be between 1 and 30 characters")
	}
	valid := false
	for _, c := range LabelColors {
		if c == color {
			valid = true
		}
	}
	if !valid {
		return Label{}, apperr.Invalid("please choose a label colour from the list")
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO labels (board_id, name, color) VALUES (?, ?, ?)`, boardID, name, color)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Label{}, apperr.Invalid("a label named %q already exists on this board", name)
		}
		return Label{}, err
	}
	id, _ := res.LastInsertId()
	return Label{ID: id, BoardID: boardID, Name: name, Color: color}, nil
}

// DeleteLabel removes a label from the board and all its tasks (admin or owner).
func (s *Service) DeleteLabel(ctx context.Context, actorID, boardID, labelID int64) error {
	if _, err := Require(ctx, s.DB, boardID, actorID, RoleAdmin); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM labels WHERE id = ? AND board_id = ?`, labelID, boardID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}
