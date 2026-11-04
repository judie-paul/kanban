package auth_test

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"kanban/internal/apperr"
	"kanban/internal/auth"
	"kanban/internal/testdb"
	"kanban/internal/users"
)

func newService(t *testing.T) *auth.Service {
	db := testdb.New(t)
	s := auth.NewService(db, &users.Service{DB: db})
	s.BcryptCost = bcrypt.MinCost
	return s
}

func TestRegisterStoresHashedPassword(t *testing.T) {
	ctx := context.Background()
	s := newService(t)

	u, err := s.Register(ctx, "  Alice   Johnson ", "Alice@Example.com", "correct horse")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.Name != "Alice Johnson" || u.Email != "alice@example.com" {
		t.Fatalf("unexpected user %+v", u)
	}
	var hash string
	if err := s.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, u.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == "correct horse" || bcrypt.CompareHashAndPassword([]byte(hash), []byte("correct horse")) != nil {
		t.Fatal("password must be stored as a bcrypt hash")
	}
}

func TestRegisterValidation(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	cases := []struct{ name, email, pw string }{
		{"", "a@example.com", "password1"},
		{"Al", "not-an-email", "password1"},
		{"Al", "a@example.com", "short"},
	}
	for _, c := range cases {
		if _, err := s.Register(ctx, c.name, c.email, c.pw); err == nil {
			t.Errorf("Register(%q,%q,%q) succeeded, want validation error", c.name, c.email, c.pw)
		} else if _, ok := apperr.IsValidation(err); !ok {
			t.Errorf("want validation error, got %v", err)
		}
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	if _, err := s.Register(ctx, "Alice", "alice@example.com", "password1"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Register(ctx, "Other Alice", "ALICE@example.com", "password2")
	if !errors.Is(err, users.ErrEmailTaken) {
		t.Fatalf("want ErrEmailTaken, got %v", err)
	}
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	if _, err := s.Register(ctx, "Alice", "alice@example.com", "password1"); err != nil {
		t.Fatal(err)
	}

	u, err := s.Authenticate(ctx, "ALICE@example.com", "password1")
	if err != nil || u.Email != "alice@example.com" {
		t.Fatalf("valid login failed: %v", err)
	}
	if _, err := s.Authenticate(ctx, "alice@example.com", "wrong-password"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong password: want ErrInvalidCredentials, got %v", err)
	}
	if _, err := s.Authenticate(ctx, "nobody@example.com", "password1"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("unknown email: want ErrInvalidCredentials, got %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	u, err := s.Register(ctx, "Alice", "alice@example.com", "password1")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.UserForSession(ctx, token)
	if err != nil || got.ID != u.ID {
		t.Fatalf("UserForSession = %v, %v", got, err)
	}

	// The raw token must not be stored.
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, token).Scan(&n)
	if n != 0 {
		t.Fatal("raw session token found in database")
	}

	if err := s.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserForSession(ctx, token); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("after logout: want ErrNotFound, got %v", err)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	u, _ := s.Register(ctx, "Alice", "alice@example.com", "password1")
	token, _, _ := s.CreateSession(ctx, u.ID)
	s.DB.Exec(`UPDATE sessions SET expires_at = '2000-01-01T00:00:00Z'`)
	if _, err := s.UserForSession(ctx, token); err == nil {
		t.Fatal("expired session accepted")
	}
	if n, _ := s.DeleteExpiredSessions(ctx); n != 1 {
		t.Fatalf("DeleteExpiredSessions removed %d, want 1", n)
	}
}

func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	u, _ := s.Register(ctx, "Alice", "alice@example.com", "password1")
	keep, _, _ := s.CreateSession(ctx, u.ID)
	other, _, _ := s.CreateSession(ctx, u.ID)

	if err := s.ChangePassword(ctx, u.ID, "wrong", "newpassword", keep); err == nil {
		t.Fatal("change with wrong current password succeeded")
	}
	if err := s.ChangePassword(ctx, u.ID, "password1", "newpassword", keep); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserForSession(ctx, keep); err != nil {
		t.Fatal("current session should survive a password change")
	}
	if _, err := s.UserForSession(ctx, other); err == nil {
		t.Fatal("other sessions should be revoked after a password change")
	}
	if _, err := s.Authenticate(ctx, "alice@example.com", "newpassword"); err != nil {
		t.Fatal("new password does not work")
	}
}
