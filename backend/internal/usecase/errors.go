// Package usecase defines domain interfaces and domain errors.
package usecase

import "errors"

var (
	// ErrNotFound is returned when a requested domain entity does not exist.
	ErrNotFound = errors.New("resource not found")

	// ErrValidation is returned when input validation fails.
	ErrValidation = errors.New("validation failed")

	// ErrUnauthorized is returned when authentication is missing or invalid.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden is returned when the user does not have permission to access the resource.
	ErrForbidden = errors.New("forbidden")

	// ErrConflict is returned when there is a state conflict (e.g. duplicate key or state).
	ErrConflict = errors.New("resource conflict")

	// ErrInternal is returned on unexpected system failures.
	ErrInternal = errors.New("internal server error")
)
