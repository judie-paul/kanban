// Package apperr defines the small set of errors that services return and
// that HTTP handlers translate into status codes and user-facing messages.
package apperr

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound means the resource does not exist OR the user is not allowed
	// to know it exists (e.g. a board they are not a member of).
	ErrNotFound = errors.New("not found")
	// ErrForbidden means the user can see the resource but lacks the role
	// required for the requested action.
	ErrForbidden = errors.New("you do not have permission to do that")
)

// ValidationError is returned when user input is invalid. Its message is safe
// to show to the user.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

// Invalid builds a ValidationError with a formatted message.
func Invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// IsValidation reports whether err is (or wraps) a ValidationError and
// returns its message.
func IsValidation(err error) (string, bool) {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Msg, true
	}
	return "", false
}
