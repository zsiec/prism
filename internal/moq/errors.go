package moq

import (
	"errors"
	"fmt"
)

// Sentinel errors for MoQ session handling. These enable callers to
// programmatically distinguish failure modes using errors.Is.
var (
	ErrVersionMismatch   = errors.New("moq: no compatible version")
	ErrUnknownTrack      = errors.New("moq: unknown track")
	ErrUnsupportedFilter = errors.New("moq: unsupported filter type")
	ErrUnknownNamespace  = errors.New("moq: unknown namespace")
)

// ParseError indicates a failure to parse a MoQ control message field.
// It wraps the underlying I/O or format error and records which field
// was being parsed when the error occurred.
type ParseError struct {
	Field string
	Err   error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("moq: parse %s: %v", e.Field, e.Err)
}

func (e *ParseError) Unwrap() error {
	return e.Err
}
