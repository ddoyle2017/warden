package warden

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/data/repo"
)

// Options configures read-only inventory and catalog access.
type Options struct {
	// DatabasePath is a literal filesystem path, absolute or relative to the
	// working directory. It is not a SQLite DSN and is never expanded or discovered.
	DatabasePath string
	// HTTPClient is reused unchanged for metadata requests. The caller owns its
	// settings and transport; Close does not close its idle connections. Nil
	// selects a reusable client with a 30-second timeout.
	HTTPClient *http.Client
}

// PackageID identifies a package by namespace and name.
type PackageID struct {
	Namespace string
	Name      string
}

// InstalledMod is an installation record, not evidence that its files exist or
// that its version is compatible with other installed packages.
type InstalledMod struct {
	Package     PackageID
	Version     string
	InstallPath string
}

// Reader reads recorded mod inventory and Thunderstore catalog metadata. Obtain
// one with Open and share it among concurrent callers; do not copy it. Its zero
// value is closed.
type Reader struct {
	mu       sync.RWMutex
	database io.Closer
	mods     repo.Mods
	catalog  *thunderstore.Client
	closeErr error
}

// Open opens an existing, compatible rollback-journal database without creating,
// migrating, or repairing it. It does not load configuration or access the network.
// A second reader for the same file in this process returns ErrDatabaseBusy,
// including when DatabasePath is a symlink or hard link to that file.
func Open(ctx context.Context, opts Options) (*Reader, error) {
	path := opts.DatabasePath
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") || strings.ContainsRune(path, '\x00') {
		return nil, fmt.Errorf("open reader: %w: an existing filesystem path is required: %w", ErrInvalidOptions, os.ErrInvalid)
	}
	database, err := repo.OpenReadOnlyDatabase(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open reader: %w", publicDatabaseError(err))
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Reader{database: database, mods: repo.NewModsRepo(database), catalog: thunderstore.New(client)}, nil
}

// Close releases the reader's SQL resources and database reservation. The
// lifecycle owner must stop and drain requests before calling Close.
// Repeated calls return the same outcome. A failed close retains ownership and
// its error; only a successful close permits another reader to open the database.
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.database == nil {
		return nil
	}
	if r.closeErr != nil {
		return r.closeErr
	}
	if err := r.database.Close(); err != nil {
		r.closeErr = fmt.Errorf("close reader: %w", publicDatabaseError(err))
		return r.closeErr
	}
	r.database = nil
	r.mods = nil
	r.catalog = nil
	return nil
}

// ListInstalledMods returns caller-owned records sorted lexicographically by
// namespace, name, version, then installation path. Values and duplicate records
// are preserved verbatim; framework rows are excluded. It returns a non-nil empty
// slice for an empty inventory, or nil and an error on any failure, never partial
// results. Calls after a successful Close return ErrClosed.
func (r *Reader) ListInstalledMods(ctx context.Context) ([]InstalledMod, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.database == nil {
		return nil, ErrClosed
	}
	if r.closeErr != nil {
		return nil, r.closeErr
	}
	records, err := r.mods.ListModsContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("list installed mods: %w", publicDatabaseError(err))
	}
	mods := make([]InstalledMod, len(records))
	for i, record := range records {
		mods[i] = InstalledMod{
			Package:     PackageID{Namespace: record.Namespace, Name: record.Name},
			Version:     record.Version,
			InstallPath: record.FilePath,
		}
	}
	slices.SortStableFunc(mods, func(a, b InstalledMod) int {
		if order := strings.Compare(a.Package.Namespace, b.Package.Namespace); order != 0 {
			return order
		}
		if order := strings.Compare(a.Package.Name, b.Package.Name); order != 0 {
			return order
		}
		if order := strings.Compare(a.Version, b.Version); order != 0 {
			return order
		}
		return strings.Compare(a.InstallPath, b.InstallPath)
	})
	return mods, nil
}
