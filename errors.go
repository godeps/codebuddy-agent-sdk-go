package codebuddy

import (
	"errors"
	"fmt"
)

// Sentinel errors.
var (
	// ErrCLINotFound is returned when the codebuddy executable cannot be located.
	ErrCLINotFound = errors.New("codebuddy: CLI executable not found")
	// ErrInitializeTimeout is returned when the CLI does not complete the
	// initialize handshake in time.
	ErrInitializeTimeout = errors.New("codebuddy: initialization timed out")
	// ErrSessionClosed is returned when operating on a closed session.
	ErrSessionClosed = errors.New("codebuddy: session closed")
	// ErrNotLoggedIn is returned when neither an API key nor a persisted CLI
	// login is available.
	ErrNotLoggedIn = errors.New("codebuddy: not authenticated (run `codebuddy login`, or set Options.Auth / CODEBUDDY_API_KEY)")
	// ErrResultError is wrapped when the CLI reports a result-level error.
	ErrResultError = errors.New("codebuddy: agent run failed")
)

// ControlRequestError is returned when a control request fails with an error
// response from the CLI.
type ControlRequestError struct {
	RequestID string
	Message   string
}

func (e *ControlRequestError) Error() string {
	return fmt.Sprintf("codebuddy: control request %s failed: %s", e.RequestID, e.Message)
}

// ResultError carries a terminal result whose subtype indicates failure.
type ResultError struct {
	Subtype   string
	Result    string
	SessionID string
}

func (e *ResultError) Error() string {
	return fmt.Sprintf("codebuddy: result %s: %s", e.Subtype, e.Result)
}

// Is reports ResultError as ErrResultError for errors.Is.
func (e *ResultError) Is(target error) bool { return target == ErrResultError }
