package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"kanban/internal/activity"
	"kanban/internal/boards"
	"kanban/internal/comments"
	"kanban/internal/notifications"
	"kanban/internal/tasks"
)

// ---- dashboard ----------------------------------------------------------

type dashboardPage struct {
	Boards        []boards.Summary
	Assigned      []tasks.Task
	Recent        []tasks.Task
	Notifications []notifications.Notification
	Totals        struct{ Tasks, Completed, Overdue int }
}

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	var p dashboardPage
	var err error
	if p.Boards, err = a.boards.ListForUser(ctx, u.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, b := range p.Boards {
		p.Totals.Tasks += b.TaskCount
		p.Totals.Completed += b.CompletedCount
		p.Totals.Overdue += b.OverdueCount
	}
	if p.Assigned, err = a.tasks.AssignedTo(ctx, u.ID, 20); err != nil {
		a.serverError(w, r, err)
		return
	}
	if p.Recent, err = a.tasks.RecentlyUpdated(ctx, u.ID, 8); err != nil {
		a.serverError(w, r, err)
		return
	}
	all, err := a.notifications.List(ctx, u.ID, 20)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, n := range all {
		if !n.IsRead && len(p.Notifications) < 5 {
			p.Notifications = append(p.Notifications, n)
		}
	}
	a.render(w, r, http.StatusOK, "dashboard", "Dashboard", p)
}

// ---- board view ---------------------------------------------------------

type columnView struct {
	boards.Column
	Tasks []tasks.Task
}

type taskDetail struct {
	Task      tasks.Task
	Comments  []comments.Comment
	Activity  []activity.Item
	CanDelete bool
	ColumnLen int // number of tasks in the task's column (for reorder buttons)
}

type boardPage struct {
	Board        boards.Board
	Role         boards.Role
	Columns      []columnView
	AllColumns   []boards.Column
	Members      []boards.Member
	Labels       []boards.Label
	Filter       tasks.Filter
	FilterActive bool
	Shown        int
	DragEnabled  bool
	Selected     *taskDetail
	filterQuery  string
}

// TaskURL links to the board with a task's detail panel open, keeping the
// current filters.
func (p boardPage) TaskURL(taskID int64) string {
	q := p.filterQuery
	if q != "" {
		q += "&"
	}
	return fmt.Sprintf("/boards/%d?%stask=%d", p.Board.ID, q, taskID)
}

// CloseURL links back to the board (with filters) without a task open.
func (p boardPage) CloseURL() string {
	if p.filterQuery == "" {
		return fmt.Sprintf("/boards/%d", p.Board.ID)
	}
	return fmt.Sprintf("/boards/%d?%s", p.Board.ID, p.filterQuery)
}

func parseFilter(q url.Values) (tasks.Filter, string) {
	id := func(k string) int64 {
		v, _ := strconv.ParseInt(q.Get(k), 10, 64)
		return v
	}
	f := tasks.Filter{
		Query:      strings.TrimSpace(q.Get("q")),
		ColumnID:   id("column"),
		AssigneeID: id("assignee"),
		CreatorID:  id("creator"),
		LabelID:    id("label"),
		Sort:       q.Get("sort"),
	}
	switch p := q.Get("priority"); p {
	case "low", "medium", "high", "urgent":
		f.Priority = p
	}
	switch d := q.Get("due"); d {
	case "overdue", "today", "week", "none":
		f.Due = d
	}
	switch f.Sort {
	case "created", "due", "priority":
	default:
		f.Sort = ""
	}
	// Rebuild a clean query string containing only recognised filters.
	clean := url.Values{}
	set := func(k, v string) {
		if v != "" && v != "0" {
			clean.Set(k, v)
		}
	}
	set("q", f.Query)
	set("column", strconv.FormatInt(f.ColumnID, 10))
	set("priority", f.Priority)
	set("assignee", strconv.FormatInt(f.AssigneeID, 10))
	set("creator", strconv.FormatInt(f.CreatorID, 10))
	set("label", strconv.FormatInt(f.LabelID, 10))
	set("due", f.Due)
	set("sort", f.Sort)
	return f, clean.Encode()
}

func (a *App) showBoard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	boardID, ok := pathID(r, "boardID")
	if !ok {
		a.notFound(w, r)
		return
	}
	board, role, err := a.boards.Get(ctx, u.ID, boardID)
	if err != nil {
		a.handleError(w, r, err, "/dashboard")
		return
	}
	p := boardPage{Board: board, Role: role}
	p.Filter, p.filterQuery = parseFilter(r.URL.Query())
	p.FilterActive = p.Filter.Active()
	p.DragEnabled = !p.FilterActive

	if p.AllColumns, err = a.boards.Columns(ctx, u.ID, boardID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if p.Members, err = a.boards.Members(ctx, u.ID, boardID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if p.Labels, err = a.boards.Labels(ctx, u.ID, boardID); err != nil {
		a.serverError(w, r, err)
		return
	}
	list, err := a.tasks.ListBoard(ctx, u.ID, boardID, p.Filter)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.Shown = len(list)
	byColumn := map[int64][]tasks.Task{}
	for _, t := range list {
		byColumn[t.ColumnID] = append(byColumn[t.ColumnID], t)
	}
	for _, c := range p.AllColumns {
		if p.Filter.ColumnID != 0 && p.Filter.ColumnID != c.ID {
			continue
		}
		p.Columns = append(p.Columns, columnView{Column: c, Tasks: byColumn[c.ID]})
	}

	if taskID, _ := strconv.ParseInt(r.URL.Query().Get("task"), 10, 64); taskID > 0 {
		d, err := a.loadTaskDetail(r, taskID)
		if err == nil && d.Task.BoardID == boardID {
			p.Selected = d
		}
	}

	title := board.Name
	if p.Selected != nil {
		title = p.Selected.Task.Title + " · " + board.Name
	}
	a.render(w, r, http.StatusOK, "board", title, p)
}

func (a *App) loadTaskDetail(r *http.Request, taskID int64) (*taskDetail, error) {
	ctx := r.Context()
	u := currentUser(r)
	t, role, err := a.tasks.Get(ctx, u.ID, taskID)
	if err != nil {
		return nil, err
	}
	d := &taskDetail{Task: t, CanDelete: t.CreatorID == u.ID || role.AtLeast(boards.RoleAdmin)}
	if d.Comments, err = a.comments.ForTask(ctx, u.ID, taskID); err != nil {
		return nil, err
	}
	if d.Activity, err = a.activity.ForTask(ctx, taskID); err != nil {
		return nil, err
	}
	a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE column_id = ?`, t.ColumnID).Scan(&d.ColumnLen)
	return d, nil
}

// ---- board CRUD ---------------------------------------------------------

func (a *App) createBoard(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	b, err := a.boards.Create(r.Context(), u.ID, r.PostFormValue("name"), r.PostFormValue("description"))
	if err != nil {
		a.handleError(w, r, err, "/dashboard")
		return
	}
	a.logger.Info("board created", "board_id", b.ID, "user_id", u.ID)
	a.setFlash(w, "success", "Board “"+b.Name+"” created. Add tasks to any column, or invite teammates from Settings.")
	http.Redirect(w, r, fmt.Sprintf("/boards/%d", b.ID), http.StatusSeeOther)
}

type settingsPage struct {
	Board   boards.Board
	Role    boards.Role
	Members []boards.Member
	Columns []boards.Column
	Counts  map[int64]int // tasks per column
	Labels  []boards.Label
}

func (a *App) boardSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	boardID, ok := pathID(r, "boardID")
	if !ok {
		a.notFound(w, r)
		return
	}
	board, role, err := a.boards.Get(ctx, u.ID, boardID)
	if err != nil {
		a.handleError(w, r, err, "/dashboard")
		return
	}
	p := settingsPage{Board: board, Role: role, Counts: map[int64]int{}}
	if p.Members, err = a.boards.Members(ctx, u.ID, boardID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if p.Columns, err = a.boards.Columns(ctx, u.ID, boardID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if p.Labels, err = a.boards.Labels(ctx, u.ID, boardID); err != nil {
		a.serverError(w, r, err)
		return
	}
	rows, err := a.db.QueryContext(ctx, `SELECT column_id, COUNT(*) FROM tasks WHERE board_id = ? GROUP BY column_id`, boardID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for rows.Next() {
		var id int64
		var n int
		rows.Scan(&id, &n)
		p.Counts[id] = n
	}
	rows.Close()
	a.render(w, r, http.StatusOK, "settings", "Settings · "+board.Name, p)
}

type boardActivityPage struct {
	Board boards.Board
	Items []activity.Item
}

func (a *App) boardActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	boardID, ok := pathID(r, "boardID")
	if !ok {
		a.notFound(w, r)
		return
	}
	board, _, err := a.boards.Get(ctx, u.ID, boardID)
	if err != nil {
		a.handleError(w, r, err, "/dashboard")
		return
	}
	items, err := a.activity.ForBoard(ctx, boardID, 300)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "activity", "Activity · "+board.Name, boardActivityPage{Board: board, Items: items})
}

func settingsURL(boardID int64) string { return fmt.Sprintf("/boards/%d/settings", boardID) }

// boardAction is a small helper for the many "do something, flash, redirect
// back to settings" handlers.
func (a *App) boardAction(w http.ResponseWriter, r *http.Request, success string, fn func(userID, boardID int64) error) {
	boardID, ok := pathID(r, "boardID")
	if !ok {
		a.notFound(w, r)
		return
	}
	back := safeRedirect(r.PostFormValue("return_to"), settingsURL(boardID))
	if err := fn(currentUser(r).ID, boardID); err != nil {
		a.handleError(w, r, err, back)
		return
	}
	if success != "" {
		a.setFlash(w, "success", success)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (a *App) updateBoard(w http.ResponseWriter, r *http.Request) {
	a.boardAction(w, r, "Board details saved.", func(uid, bid int64) error {
		return a.boards.Update(r.Context(), uid, bid, r.PostFormValue("name"), r.PostFormValue("description"))
	})
}

func (a *App) deleteBoard(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	boardID, ok := pathID(r, "boardID")
	if !ok {
		a.notFound(w, r)
		return
	}
	board, _, err := a.boards.Get(r.Context(), u.ID, boardID)
	if err != nil {
		a.handleError(w, r, err, "/dashboard")
		return
	}
	if strings.TrimSpace(r.PostFormValue("confirm_name")) != board.Name {
		a.handleError(w, r, fmtInvalid("type the board name exactly to confirm deletion"), settingsURL(boardID))
		return
	}
	if err := a.boards.Delete(r.Context(), u.ID, boardID); err != nil {
		a.handleError(w, r, err, settingsURL(boardID))
		return
	}
	a.logger.Info("board deleted", "board_id", boardID, "user_id", u.ID)
	a.setFlash(w, "success", "Board “"+board.Name+"” was deleted.")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *App) leaveBoard(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	boardID, ok := pathID(r, "boardID")
	if !ok {
		a.notFound(w, r)
		return
	}
	if err := a.boards.Leave(r.Context(), u.ID, boardID); err != nil {
		a.handleError(w, r, err, settingsURL(boardID))
		return
	}
	a.setFlash(w, "success", "You left the board.")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *App) addMember(w http.ResponseWriter, r *http.Request) {
	boardID, ok := pathID(r, "boardID")
	if !ok {
		a.notFound(w, r)
		return
	}
	m, err := a.boards.AddMember(r.Context(), currentUser(r).ID, boardID, r.PostFormValue("email"), r.PostFormValue("role"))
	if err != nil {
		a.handleError(w, r, err, settingsURL(boardID))
		return
	}
	a.setFlash(w, "success", m.Name+" was added to the board as "+m.Role.Label()+".")
	http.Redirect(w, r, settingsURL(boardID), http.StatusSeeOther)
}

func (a *App) changeMemberRole(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathID(r, "userID")
	if !ok {
		a.notFound(w, r)
		return
	}
	a.boardAction(w, r, "Role updated.", func(uid, bid int64) error {
		return a.boards.ChangeRole(r.Context(), uid, bid, userID, r.PostFormValue("role"))
	})
}

func (a *App) removeMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathID(r, "userID")
	if !ok {
		a.notFound(w, r)
		return
	}
	a.boardAction(w, r, "Member removed. Any tasks assigned to them are now unassigned.", func(uid, bid int64) error {
		return a.boards.RemoveMember(r.Context(), uid, bid, userID)
	})
}

// ---- columns & labels ---------------------------------------------------

func (a *App) createColumn(w http.ResponseWriter, r *http.Request) {
	a.boardAction(w, r, "Column added.", func(uid, bid int64) error {
		_, err := a.boards.CreateColumn(r.Context(), uid, bid, r.PostFormValue("name"), r.PostFormValue("is_done") == "on")
		return err
	})
}

func (a *App) updateColumn(w http.ResponseWriter, r *http.Request) {
	colID, ok := pathID(r, "columnID")
	if !ok {
		a.notFound(w, r)
		return
	}
	a.boardAction(w, r, "Column updated.", func(uid, bid int64) error {
		return a.boards.UpdateColumn(r.Context(), uid, bid, colID, r.PostFormValue("name"), r.PostFormValue("is_done") == "on")
	})
}

func (a *App) reorderColumn(w http.ResponseWriter, r *http.Request) {
	colID, ok := pathID(r, "columnID")
	if !ok {
		a.notFound(w, r)
		return
	}
	a.boardAction(w, r, "", func(uid, bid int64) error {
		col, err := a.columnFor(r, uid, bid, colID)
		if err != nil {
			return err
		}
		pos := col.Position
		switch r.PostFormValue("direction") {
		case "left":
			pos--
		case "right":
			pos++
		default:
			return fmtInvalid("unknown direction")
		}
		return a.boards.ReorderColumn(r.Context(), uid, bid, colID, pos)
	})
}

func (a *App) columnFor(r *http.Request, userID, boardID, colID int64) (boards.Column, error) {
	cols, err := a.boards.Columns(r.Context(), userID, boardID)
	if err != nil {
		return boards.Column{}, err
	}
	for _, c := range cols {
		if c.ID == colID {
			return c, nil
		}
	}
	return boards.Column{}, errNotFound
}

func (a *App) deleteColumn(w http.ResponseWriter, r *http.Request) {
	colID, ok := pathID(r, "columnID")
	if !ok {
		a.notFound(w, r)
		return
	}
	a.boardAction(w, r, "Column deleted.", func(uid, bid int64) error {
		return a.tasks.DeleteColumn(r.Context(), uid, bid, colID, formID(r, "move_to"))
	})
}

func (a *App) createLabel(w http.ResponseWriter, r *http.Request) {
	a.boardAction(w, r, "Label created.", func(uid, bid int64) error {
		_, err := a.boards.CreateLabel(r.Context(), uid, bid, r.PostFormValue("name"), r.PostFormValue("color"))
		return err
	})
}

func (a *App) deleteLabel(w http.ResponseWriter, r *http.Request) {
	labelID, ok := pathID(r, "labelID")
	if !ok {
		a.notFound(w, r)
		return
	}
	a.boardAction(w, r, "Label deleted.", func(uid, bid int64) error {
		return a.boards.DeleteLabel(r.Context(), uid, bid, labelID)
	})
}
