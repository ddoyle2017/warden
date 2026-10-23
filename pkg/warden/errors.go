package warden

import (
	"errors"
	"fmt"

	"github.com/ddoyle2017/warden/internal/data/repo"
)

var (
	// ErrInvalidOptions identifies an absent path or a non-filesystem connection
	// option, such as a SQLite URI or an in-memory database.
	ErrInvalidOptions = errors.New("invalid reader options")
	// ErrDatabaseUnavailable identifies missing or inaccessible database state.
	ErrDatabaseUnavailable = errors.New("database unavailable")
	// ErrDatabaseUnsupported identifies an unsupported file, schema, or stored
	// value, including WAL databases. No migration or repair is attempted.
	ErrDatabaseUnsupported = errors.New("database unsupported")
	// ErrDatabaseBusy identifies SQLite lock contention or another reader of the
	// same database in this process. The reader does not retry automatically.
	ErrDatabaseBusy = errors.New("database busy or already in use")
	// ErrDatabaseRecoveryRequired means a writable owner must recover the database.
	// The reader does not replay or modify its journal.
	ErrDatabaseRecoveryRequired = errors.New("database recovery required")
	// ErrClosed identifies an operation on a successfully closed or zero Reader.
	ErrClosed = errors.New("reader closed")
)

func publicDatabaseError(err error) error {
	for _, class := range [...]struct{ internal, public error }{
		{repo.ErrDatabaseUnavailable, ErrDatabaseUnavailable},
		{repo.ErrDatabaseUnsupported, ErrDatabaseUnsupported},
		{repo.ErrDatabaseBusy, ErrDatabaseBusy},
		{repo.ErrDatabaseRecoveryRequired, ErrDatabaseRecoveryRequired},
	} {
		if errors.Is(err, class.internal) {
			err = fmt.Errorf("%w: %w", class.public, err)
		}
	}
	return err
}
