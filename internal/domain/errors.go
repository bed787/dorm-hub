package domain

import "errors"

var (
	ErrInvalid      = errors.New("invalid input")
	ErrUnauthorized = errors.New("authentication required")
	ErrForbidden    = errors.New("access denied")
	ErrCSRF         = errors.New("csrf token expired")
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrRateLimit    = errors.New("too many requests")
)

type Error struct {
	Kind    error
	Message string
}

func (e *Error) Error() string       { return e.Message }
func (e *Error) Unwrap() error       { return e.Kind }
func Invalid(message string) error   { return &Error{ErrInvalid, message} }
func Conflict(message string) error  { return &Error{ErrConflict, message} }
func Forbidden(message string) error { return &Error{ErrForbidden, message} }

func CSRF(message string) error { return &Error{ErrCSRF, message} }
