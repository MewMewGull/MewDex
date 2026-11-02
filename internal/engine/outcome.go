package engine

import "errors"

// UnknownOutcome reports a request whose bytes may already have reached the
// engine. Callers may observe its receipt, but must not replay the operation.
type UnknownOutcome struct{ Cause error }

func (e *UnknownOutcome) Error() string {
	return "engine request outcome is unknown: " + e.Cause.Error()
}
func (e *UnknownOutcome) Unwrap() error { return e.Cause }
func IsUnknownOutcome(err error) bool   { var unknown *UnknownOutcome; return errors.As(err, &unknown) }
