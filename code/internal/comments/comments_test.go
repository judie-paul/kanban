package comments_test

import (
	"context"
	"errors"
	"testing"

	"kanban/internal/activity"
	"kanban/internal/apperr"
	"kanban/internal/boards"
	"kanban/internal/comments"
	"kanban/internal/notifications"
	"kanban/internal/tasks"
	"kanban/internal/testdb"
	"kanban/internal/users"
)

func TestCommentsActivityAndNotifications(t *testing.T) {
	ctx := context.Background()
	db := testdb.New(t)
	us := &users.Service{DB: db}
	alice, _ := us.Create(ctx, "Alice Johnson", "alice@example.com", "h")
	bob, _ := us.Create(ctx, "Bob Smith", "bob@example.com", "h")
	carol, _ := us.Create(ctx, "Carol Williams", "carol@example.com", "h")
	mallory, _ := us.Create(ctx, "Mallory", "mallory@example.com", "h")

	bs := &boards.Service{DB: db}
	ts := &tasks.Service{DB: db, Boards: bs}
	cs := &comments.Service{DB: db, Boards: bs, Tasks: ts}
	ns := &notifications.Service{DB: db}
	as := &activity.Service{DB: db}

	board, _ := bs.Create(ctx, alice.ID, "Team", "")
	bs.AddMember(ctx, alice.ID, board.ID, "bob@example.com", "member")
	bs.AddMember(ctx, alice.ID, board.ID, "carol@example.com", "member")
	cols, _ := bs.Columns(ctx, alice.ID, board.ID)
	task, err := ts.Create(ctx, alice.ID, board.ID, tasks.Input{ColumnID: cols[2].ID, Title: "Implement auth", AssigneeID: bob.ID})
	if err != nil {
		t.Fatal(err)
	}
	ns.MarkAllRead(ctx, bob.ID)

	// Carol comments: assignee (Bob) and creator (Alice) are notified.
	c, err := cs.Add(ctx, carol.ID, task.ID, "Authentication is now implemented.")
	if err != nil {
		t.Fatal(err)
	}
	if c.AuthorName != "Carol Williams" {
		t.Fatalf("author = %q", c.AuthorName)
	}
	if n, _ := ns.UnreadCount(ctx, bob.ID); n != 1 {
		t.Errorf("bob unread = %d, want 1", n)
	}
	if n, _ := ns.UnreadCount(ctx, alice.ID); n != 1 {
		t.Errorf("alice unread = %d, want 1", n)
	}
	if n, _ := ns.UnreadCount(ctx, carol.ID); n != 1 { // only the "added to board" notice
		t.Errorf("carol unread = %d, want 1 (no self-notification)", n)
	}

	h, _ := as.ForTask(ctx, task.ID)
	last := h[len(h)-1]
	if last.Action != activity.CommentAdded || last.UserName != "Carol Williams" ||
		last.Description != "commented: “Authentication is now implemented.”" {
		t.Fatalf("comment activity = %+v", last)
	}

	// Validation and access.
	if _, err := cs.Add(ctx, carol.ID, task.ID, "   "); err == nil {
		t.Error("empty comment accepted")
	}
	if _, err := cs.Add(ctx, mallory.ID, task.ID, "hi"); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("non-member comment: want ErrNotFound, got %v", err)
	}

	// Only the author may edit.
	if _, err := cs.Update(ctx, bob.ID, c.ID, "hijacked"); err == nil {
		t.Error("non-author edited a comment")
	}
	edited, err := cs.Update(ctx, carol.ID, c.ID, "Auth is done and tested.")
	if err != nil {
		t.Fatal(err)
	}
	if edited.Content != "Auth is done and tested." {
		t.Fatal("edit not saved")
	}

	// Members cannot delete others' comments; owners/admins can moderate.
	if _, err := cs.Delete(ctx, bob.ID, c.ID); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("member deleting other's comment: want ErrForbidden, got %v", err)
	}
	if _, err := cs.Delete(ctx, alice.ID, c.ID); err != nil {
		t.Fatalf("owner moderation delete: %v", err)
	}
	list, _ := cs.ForTask(ctx, alice.ID, task.ID)
	if len(list) != 0 {
		t.Fatal("comment not deleted")
	}
	h, _ = as.ForTask(ctx, task.ID)
	last = h[len(h)-1]
	if last.Action != activity.CommentDeleted || last.UserID != alice.ID || last.Description != "deleted a comment by Carol Williams" {
		t.Fatalf("delete activity = %+v", last)
	}

	// Notifications can be marked read only by their recipient.
	notes, _ := ns.List(ctx, bob.ID, 10)
	if _, err := ns.MarkRead(ctx, alice.ID, notes[0].ID); err == nil {
		t.Error("user marked someone else's notification as read")
	}
	if _, err := ns.MarkRead(ctx, bob.ID, notes[0].ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := ns.UnreadCount(ctx, bob.ID); n != 0 {
		t.Errorf("bob unread after MarkRead = %d", n)
	}
}
