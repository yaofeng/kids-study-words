package store

import "errors"

// Sentinel errors returned by store operations.
var (
	ErrNotFound      = errors.New("not found")
	ErrSetupComplete = errors.New("setup already complete")
	ErrDuplicate     = errors.New("duplicate")
	ErrLastNotebook  = errors.New("cannot delete the last notebook")
)
