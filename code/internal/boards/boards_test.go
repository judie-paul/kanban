package boards_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"kanban/internal/apperr"
	"kanban/internal/boards"
	"kanban/internal/testdb"
	"kanban/internal/users"
)

type fixture struct {
	db    *sql.DB
	svc   *boards.Service
	alice users.User // board owner
	bob   users.User
	carol users.User
}

func setup(t *testing.T) fixture {
	t.Helper()
	db := testdb.New(t)
	us := &users.Service{DB: db}
	mk := func(name, email string) users.User {
		u, err := us.Create(context.Background(), name, email, "x-hash")
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	return fixture{
		db: db, svc: &boards.Service{DB: db},
		alice: mk("Alice Johnson", "alice@example.com"),
		bob:   mk("Bob Smith", "bob@example.com"),
		carol: mk("Carol Williams", "carol@example.com"),
	}
}

func TestCreateBoardWithDefaultColumnsAndOwner(t *testing.T) {
	ctx := context.Background()
	f := setup(t)

	b, err := f.svc.Create(ctx, f.alice.ID, "Website", "Relaunch")
	if err != nil {
		t.Fatal(err)
	}
	if b.OwnerID != f.alice.ID || b.OwnerName != "Alice Johnson" {
		t.Fatalf("unexpected owner: %+v", b)
	}
	cols, err := f.svc.Columns(ctx, f.alice.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Backlog", "To Do", "In Progress", "Review", "Done"}
	if len(cols) != len(want) {
		t.Fatalf("got %d columns, want %d", len(cols), len(want))
	}
	for i, c := range cols {
		if c.Name != want[i] || c.Position != i {
			t.Errorf("column %d = %q@%d, want %q@%d", i, c.Name, c.Position, want[i], i)
		}
		if c.IsDone != (c.Name == "Done") {
			t.Errorf("column %q IsDone = %v", c.Name, c.IsDone)
		}
	}
	_, role, err := f.svc.Get(ctx, f.alice.ID, b.ID)
	if err != nil || role != boards.RoleOwner {
		t.Fatalf("owner role = %q, %v", role, err)
	}

	if _, err := f.svc.Create(ctx, f.alice.ID, "   ", ""); err == nil {
		t.Fatal("empty board name accepted")
	}
}

func TestNonMembersCannotAccessBoard(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	b, _ := f.svc.Create(ctx, f.alice.ID, "Secret", "")

	if _, _, err := f.svc.Get(ctx, f.bob.ID, b.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("Get by non-member: want ErrNotFound, got %v", err)
	}
	if _, err := f.svc.Columns(ctx, f.bob.ID, b.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("Columns by non-member: want ErrNotFound, got %v", err)
	}
	if err := f.svc.Update(ctx, f.bob.ID, b.ID, "Hacked", ""); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("Update by non-member: want ErrNotFound, got %v", err)
	}
	if err := f.svc.Delete(ctx, f.bob.ID, b.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("Delete by non-member: want ErrNotFound, got %v", err)
	}
	list, _ := f.svc.ListForUser(ctx, f.bob.ID)
	if len(list) != 0 {
		t.Fatalf("non-member sees %d boards", len(list))
	}
}

func TestAddAndRemoveMembers(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	b, _ := f.svc.Create(ctx, f.alice.ID, "Team", "")

	if _, err := f.svc.AddMember(ctx, f.alice.ID, b.ID, "BOB@example.com", "member"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddMember(ctx, f.alice.ID, b.ID, "bob@example.com", "member"); err == nil {
		t.Fatal("adding an existing member twice should fail")
	}
	if _, err := f.svc.AddMember(ctx, f.alice.ID, b.ID, "nobody@example.com", "member"); err == nil {
		t.Fatal("adding an unregistered email should fail")
	}
	if _, err := f.svc.AddMember(ctx, f.alice.ID, b.ID, "carol@example.com", "owner"); err == nil {
		t.Fatal("granting the owner role should fail")
	}

	// Bob can now see the board, and got a notification.
	if _, role, err := f.svc.Get(ctx, f.bob.ID, b.ID); err != nil || role != boards.RoleMember {
		t.Fatalf("bob: role %q err %v", role, err)
	}
	var notes int
	f.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_id = ?`, f.bob.ID).Scan(&notes)
	if notes != 1 {
		t.Fatalf("bob has %d notifications, want 1", notes)
	}

	members, _ := f.svc.Members(ctx, f.bob.ID, b.ID)
	if len(members) != 2 || members[0].Role != boards.RoleOwner {
		t.Fatalf("unexpected members %+v", members)
	}

	if err := f.svc.RemoveMember(ctx, f.alice.ID, b.ID, f.bob.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.Get(ctx, f.bob.ID, b.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatal("removed member can still access the board")
	}
	if err := f.svc.RemoveMember(ctx, f.alice.ID, b.ID, f.alice.ID); err == nil {
		t.Fatal("owner must not be removable")
	}
}

func TestRolePermissions(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	b, _ := f.svc.Create(ctx, f.alice.ID, "Team", "")
	f.svc.AddMember(ctx, f.alice.ID, b.ID, "bob@example.com", "admin")
	f.svc.AddMember(ctx, f.alice.ID, b.ID, "carol@example.com", "member")

	// Member (Carol): cannot manage settings, columns or members.
	if err := f.svc.Update(ctx, f.carol.ID, b.ID, "New", ""); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("member Update: want ErrForbidden, got %v", err)
	}
	if _, err := f.svc.CreateColumn(ctx, f.carol.ID, b.ID, "QA", false); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("member CreateColumn: want ErrForbidden, got %v", err)
	}
	if _, err := f.svc.AddMember(ctx, f.carol.ID, b.ID, "bob@example.com", "member"); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("member AddMember: want ErrForbidden, got %v", err)
	}
	// Members may create labels.
	if _, err := f.svc.CreateLabel(ctx, f.carol.ID, b.ID, "bug", "red"); err != nil {
		t.Errorf("member CreateLabel: %v", err)
	}

	// Admin (Bob): can manage settings and columns, but not members or deletion.
	if err := f.svc.Update(ctx, f.bob.ID, b.ID, "Renamed", "desc"); err != nil {
		t.Errorf("admin Update: %v", err)
	}
	if _, err := f.svc.CreateColumn(ctx, f.bob.ID, b.ID, "QA", false); err != nil {
		t.Errorf("admin CreateColumn: %v", err)
	}
	if err := f.svc.RemoveMember(ctx, f.bob.ID, b.ID, f.carol.ID); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("admin RemoveMember: want ErrForbidden, got %v", err)
	}
	if err := f.svc.Delete(ctx, f.bob.ID, b.ID); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("admin Delete: want ErrForbidden, got %v", err)
	}

	// Owner: can change roles and delete.
	if err := f.svc.ChangeRole(ctx, f.alice.ID, b.ID, f.carol.ID, "admin"); err != nil {
		t.Errorf("owner ChangeRole: %v", err)
	}
	if err := f.svc.Delete(ctx, f.alice.ID, b.ID); err != nil {
		t.Errorf("owner Delete: %v", err)
	}
	if _, _, err := f.svc.Get(ctx, f.alice.ID, b.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("board still exists after delete: %v", err)
	}
}

func TestReorderColumns(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	b, _ := f.svc.Create(ctx, f.alice.ID, "Team", "")
	cols, _ := f.svc.Columns(ctx, f.alice.ID, b.ID)

	// Move "Done" (last) to the front.
	if err := f.svc.ReorderColumn(ctx, f.alice.ID, b.ID, cols[4].ID, 0); err != nil {
		t.Fatal(err)
	}
	cols, _ = f.svc.Columns(ctx, f.alice.ID, b.ID)
	got := []string{}
	for i, c := range cols {
		got = append(got, c.Name)
		if c.Position != i {
			t.Errorf("non-contiguous position %d for %s", c.Position, c.Name)
		}
	}
	want := "Done,Backlog,To Do,In Progress,Review"
	if s := join(got); s != want {
		t.Fatalf("order = %s, want %s", s, want)
	}
}

func TestLeaveBoard(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	b, _ := f.svc.Create(ctx, f.alice.ID, "Team", "")
	f.svc.AddMember(ctx, f.alice.ID, b.ID, "bob@example.com", "member")
	if err := f.svc.Leave(ctx, f.alice.ID, b.ID); err == nil {
		t.Fatal("owner must not be able to leave")
	}
	if err := f.svc.Leave(ctx, f.bob.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := boards.IsMember(ctx, f.db, b.ID, f.bob.ID); ok {
		t.Fatal("bob is still a member after leaving")
	}
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}
