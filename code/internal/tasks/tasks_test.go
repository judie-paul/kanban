package tasks_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"kanban/internal/activity"
	"kanban/internal/apperr"
	"kanban/internal/boards"
	"kanban/internal/tasks"
	"kanban/internal/testdb"
	"kanban/internal/users"
)

type fixture struct {
	db       *sql.DB
	boards   *boards.Service
	tasks    *tasks.Service
	activity *activity.Service
	alice    users.User // owner
	bob      users.User // member
	carol    users.User // member
	mallory  users.User // not a member
	board    boards.Board
	cols     []boards.Column // Backlog, To Do, In Progress, Review, Done
}

func setup(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	db := testdb.New(t)
	us := &users.Service{DB: db}
	mk := func(name, email string) users.User {
		u, err := us.Create(ctx, name, email, "x-hash")
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	f := fixture{db: db, boards: &boards.Service{DB: db}, activity: &activity.Service{DB: db}}
	f.tasks = &tasks.Service{DB: db, Boards: f.boards}
	f.alice = mk("Alice Johnson", "alice@example.com")
	f.bob = mk("Bob Smith", "bob@example.com")
	f.carol = mk("Carol Williams", "carol@example.com")
	f.mallory = mk("Mallory", "mallory@example.com")
	var err error
	if f.board, err = f.boards.Create(ctx, f.alice.ID, "Product", ""); err != nil {
		t.Fatal(err)
	}
	for _, e := range []string{"bob@example.com", "carol@example.com"} {
		if _, err := f.boards.AddMember(ctx, f.alice.ID, f.board.ID, e, "member"); err != nil {
			t.Fatal(err)
		}
	}
	f.cols, _ = f.boards.Columns(ctx, f.alice.ID, f.board.ID)
	return f
}

func (f fixture) create(t *testing.T, actor int64, col int, title string) tasks.Task {
	t.Helper()
	task, err := f.tasks.Create(context.Background(), actor, f.board.ID, tasks.Input{
		ColumnID: f.cols[col].ID, Title: title, Priority: "medium",
	})
	if err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	return task
}

func (f fixture) history(t *testing.T, taskID int64) []activity.Item {
	t.Helper()
	items, err := f.activity.ForTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func inputFrom(task tasks.Task) tasks.Input {
	in := tasks.Input{
		Title: task.Title, Description: task.Description, Priority: string(task.Priority),
		DueDate: task.DueDate, AssigneeID: task.AssigneeID,
	}
	for _, l := range task.Labels {
		in.LabelIDs = append(in.LabelIDs, l.ID)
	}
	return in
}

func TestCreateTaskRecordsCreatorAndActivity(t *testing.T) {
	ctx := context.Background()
	f := setup(t)

	task, err := f.tasks.Create(ctx, f.alice.ID, f.board.ID, tasks.Input{
		ColumnID: f.cols[1].ID, Title: "Implement user authentication", Priority: "high",
		DueDate: "2030-01-15", AssigneeID: f.bob.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.CreatorID != f.alice.ID || task.CreatorName != "Alice Johnson" {
		t.Fatalf("creator = %d %q, want Alice", task.CreatorID, task.CreatorName)
	}
	if task.AssigneeName != "Bob Smith" || task.Priority != tasks.High || task.ColumnName != "To Do" {
		t.Fatalf("unexpected task %+v", task)
	}

	h := f.history(t, task.ID)
	if len(h) != 2 {
		t.Fatalf("got %d activity entries, want 2 (created + assigned)", len(h))
	}
	if h[0].Action != activity.TaskCreated || h[0].UserID != f.alice.ID || h[0].UserName != "Alice Johnson" {
		t.Errorf("first entry = %+v", h[0])
	}
	if h[1].Action != activity.Assigned || h[1].Description != "assigned this task to Bob Smith" {
		t.Errorf("second entry = %+v", h[1])
	}

	// Bob was notified about the assignment.
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_id = ? AND message LIKE 'assigned you%'`, f.bob.ID).Scan(&n)
	if n != 1 {
		t.Errorf("bob has %d assignment notifications, want 1", n)
	}
}

func TestCreateTaskValidation(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	bad := []tasks.Input{
		{ColumnID: f.cols[0].ID, Title: "  "},
		{ColumnID: f.cols[0].ID, Title: "x", Priority: "critical"},
		{ColumnID: f.cols[0].ID, Title: "x", DueDate: "31/12/2030"},
		{ColumnID: f.cols[0].ID, Title: "x", AssigneeID: f.mallory.ID}, // not a member
		{ColumnID: 999999, Title: "x"},
	}
	for i, in := range bad {
		if _, err := f.tasks.Create(ctx, f.alice.ID, f.board.ID, in); err == nil {
			t.Errorf("case %d: invalid input accepted", i)
		}
	}
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM task_activities`).Scan(&n)
	if n != 0 {
		t.Fatalf("failed creates left %d activity records", n)
	}
}

func TestEditTaskRecordsEachChangeAndKeepsCreator(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	task := f.create(t, f.alice.ID, 1, "Write docs")

	in := inputFrom(task)
	in.Title = "Write user docs"
	in.Description = "Cover setup and usage"
	in.Priority = "urgent"
	in.DueDate = "2030-02-01"
	updated, err := f.tasks.Update(ctx, f.bob.ID, task.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CreatorID != f.alice.ID {
		t.Fatal("editing a task must never change its creator")
	}

	h := f.history(t, task.ID)
	want := map[string]string{
		activity.TitleChanged:       "renamed this task from “Write docs” to “Write user docs”",
		activity.DescriptionChanged: "added a description",
		activity.PriorityChanged:    "changed priority from Medium to Urgent",
		activity.DueDateChanged:     "set the due date to Feb 1, 2030",
	}
	got := map[string]activity.Item{}
	for _, it := range h[1:] {
		got[it.Action] = it
	}
	for action, desc := range want {
		it, ok := got[action]
		if !ok {
			t.Errorf("missing %s entry", action)
			continue
		}
		if it.Description != desc {
			t.Errorf("%s description = %q, want %q", action, it.Description, desc)
		}
		if it.UserID != f.bob.ID {
			t.Errorf("%s attributed to user %d, want Bob (%d)", action, it.UserID, f.bob.ID)
		}
	}
	if p := got[activity.PriorityChanged]; p.OldValue.String != "medium" || p.NewValue.String != "urgent" {
		t.Errorf("priority old/new = %q/%q", p.OldValue.String, p.NewValue.String)
	}

	// Saving without changes records nothing.
	before := len(f.history(t, task.ID))
	if _, err := f.tasks.Update(ctx, f.bob.ID, task.ID, inputFrom(updated)); err != nil {
		t.Fatal(err)
	}
	if after := len(f.history(t, task.ID)); after != before {
		t.Fatalf("no-op update added %d entries", after-before)
	}
}

func TestAssignReassignUnassign(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	task := f.create(t, f.alice.ID, 0, "Fix bug")

	steps := []struct {
		assignee int64
		action   string
		desc     string
	}{
		{f.bob.ID, activity.Assigned, "assigned this task to Bob Smith"},
		{f.carol.ID, activity.Reassigned, "reassigned this task from Bob Smith to Carol Williams"},
		{0, activity.Unassigned, "unassigned Carol Williams"},
	}
	for _, s := range steps {
		in := inputFrom(task)
		in.AssigneeID = s.assignee
		var err error
		if task, err = f.tasks.Update(ctx, f.alice.ID, task.ID, in); err != nil {
			t.Fatal(err)
		}
		h := f.history(t, task.ID)
		last := h[len(h)-1]
		if last.Action != s.action || last.Description != s.desc || last.UserID != f.alice.ID {
			t.Errorf("last entry = %s %q by %d; want %s %q by Alice", last.Action, last.Description, last.UserID, s.action, s.desc)
		}
	}
	if task.CreatorID != f.alice.ID {
		t.Fatal("reassigning must not change the creator")
	}
}

func TestMoveBetweenColumnsAndCompletion(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	a := f.create(t, f.alice.ID, 1, "A")
	b := f.create(t, f.alice.ID, 1, "B")
	c := f.create(t, f.alice.ID, 1, "C")

	// Bob moves B from To Do to the top of In Progress.
	moved, err := f.tasks.Move(ctx, f.bob.ID, b.ID, f.cols[2].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if moved.ColumnName != "In Progress" || moved.Position != 0 {
		t.Fatalf("moved task at %s@%d", moved.ColumnName, moved.Position)
	}
	// Positions in To Do closed the gap: A=0, C=1.
	assertPositions(t, f, f.cols[1].ID, a.ID, c.ID)

	h := f.history(t, b.ID)
	last := h[len(h)-1]
	if last.Action != activity.Moved || last.Description != "moved this task from To Do to In Progress" || last.UserID != f.bob.ID {
		t.Fatalf("move entry = %+v", last)
	}

	// Moving into Done completes the task; moving out reopens it.
	done, err := f.tasks.Move(ctx, f.bob.ID, b.ID, f.cols[4].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if done.CompletedAt.IsZero() {
		t.Fatal("completed_at not set when moved to Done")
	}
	h = f.history(t, b.ID)
	if h[len(h)-1].Action != activity.Completed {
		t.Fatalf("want completed entry, got %s", h[len(h)-1].Action)
	}
	reopened, err := f.tasks.Move(ctx, f.carol.ID, b.ID, f.cols[3].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.CompletedAt.IsZero() {
		t.Fatal("completed_at should be cleared when reopened")
	}
	h = f.history(t, b.ID)
	if l := h[len(h)-1]; l.Action != activity.Reopened || l.UserID != f.carol.ID {
		t.Fatalf("want reopened by Carol, got %s by %d", l.Action, l.UserID)
	}

	// Alice (creator) got notifications for Bob's and Carol's moves.
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_id = ? AND task_id = ?`, f.alice.ID, b.ID).Scan(&n)
	if n != 3 {
		t.Fatalf("creator received %d notifications, want 3", n)
	}
}

func TestReorderWithinColumn(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	a := f.create(t, f.alice.ID, 0, "A")
	b := f.create(t, f.alice.ID, 0, "B")
	c := f.create(t, f.alice.ID, 0, "C")

	if _, err := f.tasks.Move(ctx, f.alice.ID, c.ID, f.cols[0].ID, 0); err != nil {
		t.Fatal(err)
	}
	assertPositions(t, f, f.cols[0].ID, c.ID, a.ID, b.ID)
	if _, err := f.tasks.Move(ctx, f.alice.ID, c.ID, f.cols[0].ID, 99); err != nil {
		t.Fatal(err)
	}
	assertPositions(t, f, f.cols[0].ID, a.ID, b.ID, c.ID)

	h := f.history(t, c.ID)
	if l := h[len(h)-1]; l.Action != activity.Reordered {
		t.Fatalf("want reordered entry, got %s", l.Action)
	}
	// Dropping a task where it already is records nothing.
	before := len(h)
	f.tasks.Move(ctx, f.alice.ID, c.ID, f.cols[0].ID, 2)
	if len(f.history(t, c.ID)) != before {
		t.Fatal("no-op move recorded activity")
	}
}

func TestTaskPermissions(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	task := f.create(t, f.bob.ID, 0, "Bob's task")

	// Non-members can neither see nor change the task.
	if _, _, err := f.tasks.Get(ctx, f.mallory.ID, task.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("non-member Get: want ErrNotFound, got %v", err)
	}
	if _, err := f.tasks.Update(ctx, f.mallory.ID, task.ID, inputFrom(task)); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("non-member Update: want ErrNotFound, got %v", err)
	}
	if _, err := f.tasks.Move(ctx, f.mallory.ID, task.ID, f.cols[1].ID, 0); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("non-member Move: want ErrNotFound, got %v", err)
	}
	if _, err := f.tasks.Create(ctx, f.mallory.ID, f.board.ID, tasks.Input{ColumnID: f.cols[0].ID, Title: "x"}); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("non-member Create: want ErrNotFound, got %v", err)
	}
	if _, err := f.tasks.ListBoard(ctx, f.mallory.ID, f.board.ID, tasks.Filter{}); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("non-member ListBoard: want ErrNotFound, got %v", err)
	}

	// A task cannot be moved into a column of another board.
	other, _ := f.boards.Create(ctx, f.bob.ID, "Other", "")
	otherCols, _ := f.boards.Columns(ctx, f.bob.ID, other.ID)
	if _, err := f.tasks.Move(ctx, f.bob.ID, task.ID, otherCols[0].ID, 0); err == nil {
		t.Error("moved a task into another board's column")
	}

	// Only the creator or an admin/owner may delete.
	if err := f.tasks.Delete(ctx, f.carol.ID, task.ID); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("other member Delete: want ErrForbidden, got %v", err)
	}
	if err := f.tasks.Delete(ctx, f.bob.ID, task.ID); err != nil {
		t.Errorf("creator Delete: %v", err)
	}
	task2 := f.create(t, f.bob.ID, 0, "Another")
	if err := f.tasks.Delete(ctx, f.alice.ID, task2.ID); err != nil {
		t.Errorf("owner Delete: %v", err)
	}
}

func TestDeletedTaskKeepsAuditTrail(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	task := f.create(t, f.alice.ID, 0, "Temporary")
	if err := f.tasks.Delete(ctx, f.alice.ID, task.ID); err != nil {
		t.Fatal(err)
	}
	items, err := f.activity.ForBoard(ctx, f.board.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Action != activity.TaskDeleted || items[0].TaskID.Valid || items[0].TaskTitle != "Temporary" {
		t.Fatalf("unexpected board history after delete: %+v", items)
	}
}

func TestActivityHistoryIsAppendOnly(t *testing.T) {
	f := setup(t)
	task := f.create(t, f.alice.ID, 0, "Audit me")
	if _, err := f.db.Exec(`UPDATE task_activities SET user_id = ? WHERE task_id = ?`, f.bob.ID, task.ID); err == nil {
		t.Fatal("activity records could be rewritten")
	}
	h := f.history(t, task.ID)
	if h[0].UserID != f.alice.ID {
		t.Fatal("activity record changed")
	}
}

func TestFailedUpdateLeavesNoActivity(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	task := f.create(t, f.alice.ID, 0, "Original")
	before := len(f.history(t, task.ID))

	// Force the final UPDATE of the task to fail *after* the activity rows
	// have been inserted within the transaction.
	if _, err := f.db.Exec(`CREATE TRIGGER fail_task_update BEFORE UPDATE OF title ON tasks
		BEGIN SELECT RAISE(ABORT, 'simulated failure'); END`); err != nil {
		t.Fatal(err)
	}
	in := inputFrom(task)
	in.Title = "Changed"
	in.Priority = "urgent"
	if _, err := f.tasks.Update(ctx, f.bob.ID, task.ID, in); err == nil || !strings.Contains(err.Error(), "simulated failure") {
		t.Fatalf("expected simulated failure, got %v", err)
	}
	if after := len(f.history(t, task.ID)); after != before {
		t.Fatalf("failed update left %d activity records", after-before)
	}
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_id = ? AND task_id = ?`, f.alice.ID, task.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("failed update left %d notifications", n)
	}
	got, _, _ := f.tasks.Get(ctx, f.alice.ID, task.ID)
	if got.Title != "Original" || got.Priority != tasks.Medium {
		t.Fatal("failed update partially changed the task")
	}
}

func TestDeleteColumnMovesTasks(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	a := f.create(t, f.alice.ID, 3, "A") // Review
	b := f.create(t, f.alice.ID, 3, "B")
	existing := f.create(t, f.alice.ID, 2, "Existing") // In Progress

	if err := f.tasks.DeleteColumn(ctx, f.alice.ID, f.board.ID, f.cols[3].ID, 0); err == nil {
		t.Fatal("deleted a non-empty column without choosing a destination")
	}
	if err := f.tasks.DeleteColumn(ctx, f.bob.ID, f.board.ID, f.cols[3].ID, f.cols[2].ID); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("member deleted a column: %v", err)
	}
	if err := f.tasks.DeleteColumn(ctx, f.alice.ID, f.board.ID, f.cols[3].ID, f.cols[2].ID); err != nil {
		t.Fatal(err)
	}
	assertPositions(t, f, f.cols[2].ID, existing.ID, a.ID, b.ID)
	h := f.history(t, a.ID)
	if l := h[len(h)-1]; !strings.Contains(l.Description, "because the column “Review” was deleted") {
		t.Fatalf("move reason missing: %q", l.Description)
	}
	cols, _ := f.boards.Columns(ctx, f.alice.ID, f.board.ID)
	if len(cols) != 4 || cols[3].Name != "Done" || cols[3].Position != 3 {
		t.Fatalf("columns after delete: %+v", cols)
	}
}

func TestFilterAndSort(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	mk := func(title, prio, due string, assignee int64, actor int64) {
		_, err := f.tasks.Create(ctx, actor, f.board.ID, tasks.Input{
			ColumnID: f.cols[1].ID, Title: title, Priority: prio, DueDate: due, AssigneeID: assignee,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	mk("Login page", "high", "2000-01-01", f.bob.ID, f.alice.ID) // overdue
	mk("Signup page", "low", "2099-01-01", 0, f.bob.ID)
	mk("Database 100% done_", "urgent", "", f.carol.ID, f.alice.ID)

	count := func(fl tasks.Filter) int {
		ts, err := f.tasks.ListBoard(ctx, f.alice.ID, f.board.ID, fl)
		if err != nil {
			t.Fatal(err)
		}
		return len(ts)
	}
	checks := []struct {
		name string
		f    tasks.Filter
		want int
	}{
		{"all", tasks.Filter{}, 3},
		{"search", tasks.Filter{Query: "PAGE"}, 2},
		{"search literal %", tasks.Filter{Query: "100%"}, 1},
		{"search literal _", tasks.Filter{Query: "n_p"}, 0},
		{"priority", tasks.Filter{Priority: "high"}, 1},
		{"assignee", tasks.Filter{AssigneeID: f.bob.ID}, 1},
		{"unassigned", tasks.Filter{AssigneeID: -1}, 1},
		{"creator", tasks.Filter{CreatorID: f.bob.ID}, 1},
		{"overdue", tasks.Filter{Due: "overdue"}, 1},
		{"no due date", tasks.Filter{Due: "none"}, 1},
		{"column", tasks.Filter{ColumnID: f.cols[0].ID}, 0},
	}
	for _, c := range checks {
		if got := count(c.f); got != c.want {
			t.Errorf("%s: got %d tasks, want %d", c.name, got, c.want)
		}
	}

	ts, _ := f.tasks.ListBoard(ctx, f.alice.ID, f.board.ID, tasks.Filter{Sort: "priority"})
	if ts[0].Priority != tasks.Urgent || ts[2].Priority != tasks.Low {
		t.Errorf("priority sort wrong: %s, %s, %s", ts[0].Priority, ts[1].Priority, ts[2].Priority)
	}
	ts, _ = f.tasks.ListBoard(ctx, f.alice.ID, f.board.ID, tasks.Filter{Sort: "due"})
	if ts[0].Title != "Login page" || ts[2].DueDate != "" {
		t.Errorf("due sort wrong: %q first, %q last", ts[0].Title, ts[2].Title)
	}
	if !ts[0].IsOverdue() {
		t.Error("task due in 2000 should be overdue")
	}
}

func TestLabels(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	bug, _ := f.boards.CreateLabel(ctx, f.alice.ID, f.board.ID, "bug", "red")
	ui, _ := f.boards.CreateLabel(ctx, f.alice.ID, f.board.ID, "ui", "blue")
	task := f.create(t, f.alice.ID, 0, "Styled button")

	in := inputFrom(task)
	in.LabelIDs = []int64{bug.ID, ui.ID}
	task, err := f.tasks.Update(ctx, f.bob.ID, task.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Labels) != 2 {
		t.Fatalf("task has %d labels", len(task.Labels))
	}
	h := f.history(t, task.ID)
	if l := h[len(h)-1]; l.Action != activity.LabelsChanged || l.Description != "added the labels “bug”, “ui”" {
		t.Fatalf("labels entry = %q", l.Description)
	}

	ts, _ := f.tasks.ListBoard(ctx, f.alice.ID, f.board.ID, tasks.Filter{LabelID: ui.ID})
	if len(ts) != 1 || len(ts[0].Labels) != 2 {
		t.Fatalf("label filter returned %+v", ts)
	}
}

func assertPositions(t *testing.T, f fixture, columnID int64, ids ...int64) {
	t.Helper()
	rows, err := f.db.Query(`SELECT id, position FROM tasks WHERE column_id = ? ORDER BY position`, columnID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var id int64
		var pos int
		rows.Scan(&id, &pos)
		if i >= len(ids) || id != ids[i] || pos != i {
			t.Fatalf("position %d: got task %d@%d, want order %v", i, id, pos, ids)
		}
		i++
	}
	if i != len(ids) {
		t.Fatalf("column has %d tasks, want %d", i, len(ids))
	}
}

func TestDeleteBoardWithTasksCascades(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	task := f.create(t, f.alice.ID, 1, "Will vanish")
	in := inputFrom(task)
	in.AssigneeID = f.bob.ID
	if _, err := f.tasks.Update(ctx, f.alice.ID, task.ID, in); err != nil {
		t.Fatal(err)
	}
	if err := f.boards.Delete(ctx, f.alice.ID, f.board.ID); err != nil {
		t.Fatalf("delete board with tasks: %v", err)
	}
	for _, table := range []string{"tasks", "columns", "task_activities", "board_members", "notifications"} {
		var n int
		f.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
		if n != 0 {
			t.Errorf("%s still has %d rows after board deletion", table, n)
		}
	}
}
