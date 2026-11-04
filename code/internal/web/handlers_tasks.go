package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"kanban/internal/apperr"
	"kanban/internal/tasks"
)

var errNotFound = apperr.ErrNotFound

func fmtInvalid(msg string) error { return apperr.Invalid("%s", msg) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func taskURL(boardID, taskID int64) string {
	return fmt.Sprintf("/boards/%d?task=%d", boardID, taskID)
}

// taskInput reads the task form. The creator is deliberately NOT read from
// the form: it always comes from the session.
func taskInput(r *http.Request) tasks.Input {
	in := tasks.Input{
		ColumnID:    formID(r, "column_id"),
		Title:       r.PostFormValue("title"),
		Description: r.PostFormValue("description"),
		Priority:    r.PostFormValue("priority"),
		DueDate:     r.PostFormValue("due_date"),
		AssigneeID:  formID(r, "assignee_id"),
	}
	for _, v := range r.PostForm["labels"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.LabelIDs = append(in.LabelIDs, id)
		}
	}
	return in
}

func (a *App) createTask(w http.ResponseWriter, r *http.Request) {
	boardID, ok := pathID(r, "boardID")
	if !ok {
		a.notFound(w, r)
		return
	}
	r.ParseForm()
	back := safeRedirect(r.PostFormValue("return_to"), fmt.Sprintf("/boards/%d", boardID))
	t, err := a.tasks.Create(r.Context(), currentUser(r).ID, boardID, taskInput(r))
	if err != nil {
		a.handleError(w, r, err, back)
		return
	}
	a.setFlash(w, "success", "Task “"+t.Title+"” created.")
	http.Redirect(w, r, taskURL(boardID, t.ID), http.StatusSeeOther)
}

// showTask is the stable link to a task (used in notifications and the
// dashboard). It opens the board with the task's detail panel.
func (a *App) showTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := pathID(r, "taskID")
	if !ok {
		a.notFound(w, r)
		return
	}
	t, _, err := a.tasks.Get(r.Context(), currentUser(r).ID, taskID)
	if err != nil {
		a.handleError(w, r, err, "/dashboard")
		return
	}
	http.Redirect(w, r, taskURL(t.BoardID, t.ID), http.StatusSeeOther)
}

// taskBack works out where to send the user after a task action.
func (a *App) taskBack(r *http.Request, taskID int64) string {
	if rt := r.PostFormValue("return_to"); rt != "" {
		return safeRedirect(rt, "/dashboard")
	}
	return fmt.Sprintf("/tasks/%d", taskID)
}

func (a *App) updateTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := pathID(r, "taskID")
	if !ok {
		a.notFound(w, r)
		return
	}
	r.ParseForm()
	back := a.taskBack(r, taskID)
	if _, err := a.tasks.Update(r.Context(), currentUser(r).ID, taskID, taskInput(r)); err != nil {
		a.handleError(w, r, err, back)
		return
	}
	a.setFlash(w, "success", "Task saved.")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// moveTask serves both the drag-and-drop fetch request (JSON response) and
// the accessible "Move" form (redirect).
func (a *App) moveTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := pathID(r, "taskID")
	if !ok {
		a.notFound(w, r)
		return
	}
	back := a.taskBack(r, taskID)
	pos, err := strconv.Atoi(r.PostFormValue("position"))
	if err != nil {
		a.handleError(w, r, fmtInvalid("invalid position"), back)
		return
	}
	t, err := a.tasks.Move(r.Context(), currentUser(r).ID, taskID, formID(r, "column_id"), pos)
	if err != nil {
		a.handleError(w, r, err, back)
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "column_id": t.ColumnID, "position": t.Position, "column": t.ColumnName,
		})
		return
	}
	a.setFlash(w, "success", "Task moved to "+t.ColumnName+".")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (a *App) deleteTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := pathID(r, "taskID")
	if !ok {
		a.notFound(w, r)
		return
	}
	u := currentUser(r)
	t, _, err := a.tasks.Get(r.Context(), u.ID, taskID)
	if err != nil {
		a.handleError(w, r, err, "/dashboard")
		return
	}
	board := fmt.Sprintf("/boards/%d", t.BoardID)
	if err := a.tasks.Delete(r.Context(), u.ID, taskID); err != nil {
		a.handleError(w, r, err, taskURL(t.BoardID, t.ID))
		return
	}
	a.setFlash(w, "success", "Task “"+t.Title+"” was deleted. The deletion is recorded in the board activity log.")
	http.Redirect(w, r, board, http.StatusSeeOther)
}

// ---- comments -----------------------------------------------------------

func (a *App) addComment(w http.ResponseWriter, r *http.Request) {
	taskID, ok := pathID(r, "taskID")
	if !ok {
		a.notFound(w, r)
		return
	}
	back := a.taskBack(r, taskID)
	if _, err := a.comments.Add(r.Context(), currentUser(r).ID, taskID, r.PostFormValue("content")); err != nil {
		a.handleError(w, r, err, back)
		return
	}
	http.Redirect(w, r, back+"#comments", http.StatusSeeOther)
}

func (a *App) updateComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "commentID")
	if !ok {
		a.notFound(w, r)
		return
	}
	c, err := a.comments.Get(r.Context(), currentUser(r).ID, id)
	if err != nil {
		a.handleError(w, r, err, "/dashboard")
		return
	}
	back := a.taskBack(r, c.TaskID)
	if _, err := a.comments.Update(r.Context(), currentUser(r).ID, id, r.PostFormValue("content")); err != nil {
		a.handleError(w, r, err, back)
		return
	}
	a.setFlash(w, "success", "Comment updated.")
	http.Redirect(w, r, back+"#comments", http.StatusSeeOther)
}

func (a *App) deleteComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "commentID")
	if !ok {
		a.notFound(w, r)
		return
	}
	c, err := a.comments.Get(r.Context(), currentUser(r).ID, id)
	if err != nil {
		a.handleError(w, r, err, "/dashboard")
		return
	}
	back := a.taskBack(r, c.TaskID)
	if _, err := a.comments.Delete(r.Context(), currentUser(r).ID, id); err != nil {
		a.handleError(w, r, err, back)
		return
	}
	a.setFlash(w, "success", "Comment deleted.")
	http.Redirect(w, r, back+"#comments", http.StatusSeeOther)
}

// ---- notifications ------------------------------------------------------

func (a *App) listNotifications(w http.ResponseWriter, r *http.Request) {
	list, err := a.notifications.List(r.Context(), currentUser(r).ID, 100)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "notifications", "Notifications", list)
}

// markNotificationRead marks a notification read and, when it refers to a
// task, opens that task.
func (a *App) markNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "notificationID")
	if !ok {
		a.notFound(w, r)
		return
	}
	n, err := a.notifications.MarkRead(r.Context(), currentUser(r).ID, id)
	if err != nil {
		a.handleError(w, r, err, "/notifications")
		return
	}
	target := safeRedirect(r.PostFormValue("return_to"), "/notifications")
	if r.PostFormValue("open") == "1" {
		switch {
		case n.TaskID.Valid:
			target = fmt.Sprintf("/tasks/%d", n.TaskID.Int64)
		case n.BoardID.Valid:
			target = fmt.Sprintf("/boards/%d", n.BoardID.Int64)
		}
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (a *App) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if err := a.notifications.MarkAllRead(r.Context(), currentUser(r).ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.setFlash(w, "success", "All notifications marked as read.")
	http.Redirect(w, r, safeRedirect(r.PostFormValue("return_to"), "/notifications"), http.StatusSeeOther)
}
