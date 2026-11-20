// Package domain holds shared types and errors. It has no dependencies.
package domain

import "errors"

// Sentinel errors. Adapters (HTTP, CLI, MCP) map them centrally.
var (
	ErrNotFound    = &DomainError{Code: "not_found", Msg: "not found"}
	ErrConflict    = &DomainError{Code: "conflict", Msg: "conflict"}
	ErrInvalid     = &DomainError{Code: "invalid", Msg: "invalid input"}
	ErrUnavailable = &DomainError{Code: "unavailable", Msg: "unavailable"}
)

// DomainError is an error with a stable machine-readable code.
type DomainError struct { //nolint:revive // name is part of the documented API
	Code string
	Msg  string
}

func (e *DomainError) Error() string { return e.Msg }

// Is reports whether target is a DomainError with the same Code, so a
// DomainError with a custom message still matches its sentinel.
func (e *DomainError) Is(target error) bool {
	t, ok := target.(*DomainError)
	return ok && t.Code == e.Code
}

// New returns a DomainError with the same code as base and a custom message.
func New(base *DomainError, msg string) *DomainError {
	return &DomainError{Code: base.Code, Msg: msg}
}

// Code returns the domain code of err, or "" if err is not a DomainError.
func Code(err error) string {
	var d *DomainError
	if errors.As(err, &d) {
		return d.Code
	}
	return ""
}
