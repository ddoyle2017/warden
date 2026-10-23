package warden

import (
	"context"
	"errors"
	"testing"

	"github.com/ddoyle2017/warden/internal/data/repo"
)

func TestCloseFailureRetainsOwnership(t *testing.T) {
	cause := errors.New("connection did not close")
	calls := 0
	reader := &Reader{database: closeFunc(func() error {
		calls++
		return errors.Join(repo.ErrDatabaseUnavailable, cause)
	})}
	for attempt := 0; attempt < 2; attempt++ {
		if err := reader.Close(); !errors.Is(err, cause) || !errors.Is(err, ErrDatabaseUnavailable) {
			t.Fatalf("close = %v", err)
		}
		got, err := reader.ListInstalledMods(context.Background())
		if got != nil || !errors.Is(err, cause) || errors.Is(err, ErrClosed) {
			t.Errorf("failed-close inventory = %#v, %v", got, err)
		}
		if reader.database == nil || calls != 1 {
			t.Fatalf("failed close forgot its owner or retried: database=%v, calls=%d", reader.database, calls)
		}
	}
}

func TestPublicDatabaseErrorPreservesJoinedCauses(t *testing.T) {
	cause := errors.New("connection failure")
	err := publicDatabaseError(errors.Join(repo.ErrDatabaseUnsupported, repo.ErrDatabaseUnavailable, context.Canceled, cause))
	for _, want := range []error{ErrDatabaseUnsupported, ErrDatabaseUnavailable, repo.ErrDatabaseUnsupported,
		repo.ErrDatabaseUnavailable, context.Canceled, cause} {
		if !errors.Is(err, want) {
			t.Errorf("missing classification or cause %v: %v", want, err)
		}
	}
}

type closeFunc func() error

func (f closeFunc) Close() error { return f() }
