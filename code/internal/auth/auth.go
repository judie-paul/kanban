// Package auth handles registration, login, password hashing and sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"kanban/internal/apperr"
	"kanban/internal/database"
	"kanban/internal/users"
)

// SessionDuration is how long a login lasts.
const SessionDuration = 7 * 24 * time.Hour

// MinPasswordLength is the minimum accepted password length.
const MinPasswordLength = 8

// ErrInvalidCredentials is returned for a wrong email or password. The same
// error is used for both so attackers cannot discover which emails exist.
var ErrInvalidCredentials = apperr.Invalid("incorrect email or password")

// Service implements authentication on top of the users table.
type Service struct {
	DB    *sql.DB
	Users *users.Service
	// BcryptCost may be lowered in tests to make them fast.
	BcryptCost int
}

// NewService returns an auth service using bcrypt's default cost.
func NewService(db *sql.DB, u *users.Service) *Service {
	return &Service{DB: db, Users: u, BcryptCost: bcrypt.DefaultCost}
}

// ValidatePassword checks password strength rules.
func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return apperr.Invalid("password must be at least %d characters", MinPasswordLength)
	}
	if len(password) > 72 { // bcrypt only uses the first 72 bytes
		return apperr.Invalid("password must be 72 bytes or fewer")
	}
	return nil
}

// HashPassword hashes a password with bcrypt.
func (s *Service) HashPassword(password string) (string, error) {
	cost := s.BcryptCost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	return string(h), err
}

// Register validates input, hashes the password and creates the user.
func (s *Service) Register(ctx context.Context, name, email, password string) (users.User, error) {
	if _, err := users.ValidateName(name); err != nil {
		return users.User{}, err
	}
	if _, err := users.ValidateEmail(email); err != nil {
		return users.User{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return users.User{}, err
	}
	hash, err := s.HashPassword(password)
	if err != nil {
		return users.User{}, err
	}
	return s.Users.Create(ctx, name, email, hash)
}

// dummyHash is compared against when the email does not exist so that login
// takes the same time whether or not the account exists.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)

// Authenticate checks an email/password pair.
func (s *Service) Authenticate(ctx context.Context, email, password string) (users.User, error) {
	u, hash, err := s.Users.GetCredentials(ctx, email)
	if errors.Is(err, apperr.ErrNotFound) {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return users.User{}, ErrInvalidCredentials
	}
	if err != nil {
		return users.User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return users.User{}, ErrInvalidCredentials
	}
	return u, nil
}

// ChangePassword verifies the current password and stores a new one.
// All other sessions of the user are revoked.
func (s *Service) ChangePassword(ctx context.Context, userID int64, current, next, keepToken string) error {
	hash, err := s.Users.GetPasswordHash(ctx, userID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return apperr.Invalid("your current password is incorrect")
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	newHash, err := s.HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.Users.UpdatePasswordHash(ctx, userID, newHash); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, hashToken(keepToken))
	return err
}

// CreateSession starts a session for the user and returns the raw token that
// must be sent to the browser in a cookie. Only its hash is stored.
func (s *Service) CreateSession(ctx context.Context, userID int64) (token string, expires time.Time, err error) {
	token, err = RandomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expires = now.Add(SessionDuration)
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hashToken(token), userID, now.Format(database.TimeLayout), expires.Format(database.TimeLayout))
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// UserForSession returns the user owning a valid, unexpired session token.
func (s *Service) UserForSession(ctx context.Context, token string) (users.User, error) {
	if token == "" {
		return users.User{}, apperr.ErrNotFound
	}
	var u users.User
	err := s.DB.QueryRowContext(ctx, `
		SELECT u.id, u.name, u.email, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`,
		hashToken(token), database.Now()).
		Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return users.User{}, apperr.ErrNotFound
	}
	return u, err
}

// DeleteSession ends a session (logout).
func (s *Service) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// DeleteExpiredSessions removes stale sessions. Called periodically.
func (s *Service) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, database.Now())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RandomToken returns 32 cryptographically random bytes, URL-safe encoded.
func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
