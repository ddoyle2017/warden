package service

import "errors"

// Only direct no-work results classify an operation. Nested matches can be causes
// of a failure rather than evidence that the enclosing request was cancelled.
var (
	// ErrCancelled reports a deliberate refusal before the requested mutation.
	ErrCancelled = errors.New("operation cancelled")
	// ErrNoChanges reports a known no-op, not a completed mutation or a failed lookup.
	ErrNoChanges = errors.New("no changes needed")
)
