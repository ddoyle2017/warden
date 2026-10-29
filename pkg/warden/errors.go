package warden

import (
	"errors"
	"fmt"

	"github.com/ddoyle2017/warden/internal/api"
	"github.com/ddoyle2017/warden/internal/api/thunderstore"
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
	// ErrInvalidPackageID identifies invalid caller-supplied namespace/name components.
	ErrInvalidPackageID = errors.New("invalid package identity")
	// ErrPackageNotFound identifies a catalog HTTP 404 response.
	ErrPackageNotFound = errors.New("package not found")
	// ErrInvalidMetadata identifies malformed, incomplete, or mismatched catalog
	// data, or installed identities, duplicates, or versions that prevent assessment.
	ErrInvalidMetadata = errors.New("invalid package metadata")
	// ErrRemoteFailure identifies request, unexpected HTTP status, or response
	// body failures. Underlying transport and context causes remain available.
	ErrRemoteFailure = errors.New("remote package lookup failed")
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

func publicCatalogError(err error) error {
	for _, class := range [...]struct{ internal, public error }{
		{thunderstore.ErrPackageNotFound, ErrPackageNotFound},
		{api.ErrJSONParse, ErrInvalidMetadata},
	} {
		if errors.Is(err, class.internal) {
			err = fmt.Errorf("%w: %w", class.public, err)
		}
	}
	if errors.Is(err, api.ErrHTTPClient) || errors.Is(err, api.ErrByteIO) || errors.Is(err, thunderstore.ErrThunderstoreAPI) {
		err = fmt.Errorf("%w: %w", ErrRemoteFailure, err)
	}
	return err
}
