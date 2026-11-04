// Package users stores and retrieves user accounts.
package users

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"kanban/internal/apperr"
	"kanban/internal/database"
)

// User is a registered account. The password hash never leaves this package
// except through GetCredentials.
type User struct {
	ID        int64
	Name      string
	Email     string
	CreatedAt database.Time
}

// Initials returns up to two uppercase initials for avatar badges.
func (u User) Initials() string {
	return Initials(u.Name)
}

// Initials returns up to two uppercase initials of a full name.
func Initials(name string) string {
	parts := strings.Fields(name)
	out := ""
	for i, p := range parts {
		if i > 0 && i < len(parts)-1 {
			continue // first and last name only
		}
		r, _ := utf8.DecodeRuneInString(p)
		out += strings.ToUpper(string(r))
	}
	if out == "" {
		return "?"
	}
	return out
}

// ErrEmailTaken is returned when registering an email that already exists.
var ErrEmailTaken = apperr.Invalid("an account with that email address already exists")

// Service provides access to the users table.
type Service struct {
	DB *sql.DB
}

// NormalizeEmail trims and lower-cases an email address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateName checks a display name.
func ValidateName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", apperr.Invalid("please enter your full name")
	}
	if utf8.RuneCountInString(name) > 100 {
		return "", apperr.Invalid("name must be 100 characters or fewer")
	}
	return name, nil
}

// ValidateEmail checks and normalizes an email address.
func ValidateEmail(email string) (string, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return "", apperr.Invalid("please enter your email address")
	}
	if len(email) > 254 {
		return "", apperr.Invalid("email address is too long")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || !strings.Contains(email[strings.LastIndex(email, "@"):], ".") {
		return "", apperr.Invalid("please enter a valid email address")
	}
	return email, nil
}

// Create inserts a new user with an already-hashed password.
func (s *Service) Create(ctx context.Context, name, email, passwordHash string) (User, error) {
	name, err := ValidateName(name)
	if err != nil {
		return User{}, err
	}
	email, err = ValidateEmail(email)
	if err != nil {
		return User{}, err
	}
	now := database.Now()
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO users (name, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		name, email, passwordHash, now)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return s.GetByID(ctx, id)
}

// GetByID returns a user by ID.
func (s *Service) GetByID(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, name, email, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, apperr.ErrNotFound
	}
	return u, err
}

// GetByEmail returns a user by (case-insensitive) email.
func (s *Service) GetByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, name, email, created_at FROM users WHERE email = ?`, NormalizeEmail(email)).
		Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, apperr.ErrNotFound
	}
	return u, err
}

// GetCredentials returns the user and password hash for an email address.
func (s *Service) GetCredentials(ctx context.Context, email string) (User, string, error) {
	var u User
	var hash string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, name, email, created_at, password_hash FROM users WHERE email = ?`, NormalizeEmail(email)).
		Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", apperr.ErrNotFound
	}
	return u, hash, err
}

// GetPasswordHash returns the stored hash for a user ID.
func (s *Service) GetPasswordHash(ctx context.Context, id int64) (string, error) {
	var hash string
	err := s.DB.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", apperr.ErrNotFound
	}
	return hash, err
}

// UpdateName changes a user's display name.
func (s *Service) UpdateName(ctx context.Context, id int64, name string) error {
	name, err := ValidateName(name)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE users SET name = ? WHERE id = ?`, name, id)
	return err
}

// UpdatePasswordHash replaces a user's password hash.
func (s *Service) UpdatePasswordHash(ctx context.Context, id int64, hash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
