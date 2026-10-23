package repo

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mattn/go-sqlite3"
)

var (
	ErrDatabaseUnavailable      = errors.New("database is missing or unavailable")
	ErrDatabaseUnsupported      = errors.New("database format or mods schema is unsupported")
	ErrDatabaseBusy             = errors.New("database is busy")
	ErrDatabaseRecoveryRequired = errors.New("database requires recovery by a writable owner")
	ErrDatabaseCancelled        = errors.New("database operation was cancelled")
)

// ReadOnlyDatabase owns one connection pool and a process-local file reservation.
// Reuse it for concurrent reads; opening the same file again returns ErrDatabaseBusy.
type ReadOnlyDatabase struct {
	db          *sql.DB
	connector   *readOnlyConnector
	reservation *readOnlyReservation
	closeOnce   sync.Once
	closeErr    error
}

// OpenReadOnlyDatabase opens an existing rollback-journal database without
// initializing or repairing it. The caller owns Close and must close query rows.
// Concurrent writers must be separate processes, such as the Warden CLI.
func OpenReadOnlyDatabase(ctx context.Context, path string) (*ReadOnlyDatabase, error) {
	if err := ctx.Err(); err != nil {
		return nil, classifyDatabaseError(err)
	}
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil, fmt.Errorf("%w: an existing filesystem path is required: %w", ErrDatabaseUnsupported, os.ErrInvalid)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	reservation, err := reserveReadOnlyDatabase(absolute)
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	uri := url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(absolute),
		RawQuery: url.Values{
			"mode":          {"ro"},
			"cache":         {"private"},
			"_query_only":   {"1"},
			"_busy_timeout": {"0"},
		}.Encode(),
	}
	connector := &readOnlyConnector{
		path: absolute, dsn: uri.String(), sqlite: &sqlite3.SQLiteDriver{}, info: reservation.info,
	}
	db := sql.OpenDB(connector)
	// Header inspection must not close a separate file descriptor while this
	// pool holds SQLite locks: POSIX close would release this process's locks.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	owned := &ReadOnlyDatabase{db: db, connector: connector, reservation: reservation}
	if err := validateModsSchema(ctx, db); err != nil {
		return nil, classifyDatabaseError(errors.Join(err, owned.Close()))
	}
	return owned, nil
}

func (d *ReadOnlyDatabase) Query(query string, args ...any) (*sql.Rows, error) {
	return d.QueryContext(context.Background(), query, args...)
}

func (d *ReadOnlyDatabase) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := d.db.QueryContext(ctx, query, args...)
	return rows, classifyDatabaseError(err)
}

func (d *ReadOnlyDatabase) Prepare(query string) (*sql.Stmt, error) {
	return d.db.Prepare(query)
}

func (d *ReadOnlyDatabase) Begin() (*sql.Tx, error) {
	return d.db.Begin()
}

// Close prevents new operations and waits for SQL resources to close before
// releasing the file reservation. Close is safe to call more than once.
func (d *ReadOnlyDatabase) Close() error {
	d.closeOnce.Do(func() {
		d.connector.mu.Lock()
		d.connector.closed = true
		d.connector.mu.Unlock()
		err := d.db.Close()
		d.connector.active.Wait()
		d.connector.mu.Lock()
		err = errors.Join(err, d.connector.closeErr)
		d.connector.mu.Unlock()
		if err != nil {
			d.closeErr = classifyDatabaseError(fmt.Errorf("close reader; file reservation retained: %w", err))
			return
		}
		readOnlyReservations.Lock()
		delete(readOnlyReservations.files, d.reservation)
		readOnlyReservations.Unlock()
	})
	return d.closeErr
}

type readOnlyReservation struct {
	path string
	info os.FileInfo
}

// Raw header descriptors must never close while another library reader holds
// POSIX locks on the same inode, even through a symlink or hard link.
var readOnlyReservations = struct {
	sync.Mutex
	files map[*readOnlyReservation]struct{}
}{files: make(map[*readOnlyReservation]struct{})}

func reserveReadOnlyDatabase(path string) (*readOnlyReservation, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q is not a regular file: %w", ErrDatabaseUnsupported, path, os.ErrInvalid)
	}
	readOnlyReservations.Lock()
	defer readOnlyReservations.Unlock()
	for reservation := range readOnlyReservations.files {
		if path == reservation.path || os.SameFile(info, reservation.info) {
			return nil, fmt.Errorf("%w: database already has a reader in this process", ErrDatabaseBusy)
		}
	}
	reservation := &readOnlyReservation{path: path, info: info}
	readOnlyReservations.files[reservation] = struct{}{}
	return reservation, nil
}

type readOnlyConnector struct {
	path     string
	dsn      string
	sqlite   *sqlite3.SQLiteDriver
	info     os.FileInfo
	mu       sync.Mutex
	active   sync.WaitGroup
	closed   bool
	closeErr error
}

func (c *readOnlyConnector) Connect(ctx context.Context) (driver.Conn, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, classifyDatabaseError(sql.ErrConnDone)
	}
	if c.closeErr != nil {
		err := c.closeErr
		c.mu.Unlock()
		return nil, classifyDatabaseError(fmt.Errorf("previous connection did not close: %w", err))
	}
	c.active.Add(1)
	c.mu.Unlock()
	connected := false
	defer func() {
		if !connected {
			c.active.Done()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, classifyDatabaseError(err)
	}
	// SQLiteDriver.Open itself executes PRAGMAs, before Ping or ConnectHook.
	if err := inspectDatabaseHeader(c.path, c.info); err != nil {
		return nil, classifyDatabaseError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, classifyDatabaseError(err)
	}
	conn, err := c.sqlite.Open(c.dsn)
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	if err := ctx.Err(); err != nil {
		closeErr := conn.Close()
		c.recordCloseError(closeErr)
		return nil, classifyDatabaseError(errors.Join(err, closeErr))
	}
	connected = true
	return &readOnlyConnection{SQLiteConn: conn.(*sqlite3.SQLiteConn), connector: c}, nil
}

func (c *readOnlyConnector) Driver() driver.Driver {
	return c.sqlite
}

func (c *readOnlyConnector) recordCloseError(err error) {
	c.mu.Lock()
	c.closeErr = errors.Join(c.closeErr, err)
	c.mu.Unlock()
}

type readOnlyConnection struct {
	*sqlite3.SQLiteConn
	connector *readOnlyConnector
}

func (c *readOnlyConnection) Close() error {
	err := c.SQLiteConn.Close()
	c.connector.recordCloseError(err)
	c.connector.active.Done()
	return err
}

func inspectDatabaseHeader(path string, expected os.FileInfo) (err error) {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(info, expected) {
		return fmt.Errorf("%w: database file was replaced; close and reopen the reader", ErrDatabaseUnsupported)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	var header [100]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return fmt.Errorf("%w: read SQLite header: %w", ErrDatabaseUnsupported, err)
	}
	if string(header[:16]) != "SQLite format 3\x00" {
		return fmt.Errorf("%w: invalid SQLite header", ErrDatabaseUnsupported)
	}
	if header[18] == 2 || header[19] == 2 {
		return fmt.Errorf("%w: WAL databases are not supported", ErrDatabaseUnsupported)
	}
	pageSize := int(binary.BigEndian.Uint16(header[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	schemaFormat := binary.BigEndian.Uint32(header[44:48])
	encoding := binary.BigEndian.Uint32(header[56:60])
	// Only invariant header fields are checked. Page counts and change counters
	// can legitimately change during ordinary rollback-journal writes.
	if header[18] != 1 || header[19] != 1 ||
		pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 ||
		pageSize-int(header[20]) < 480 || info.Size() < int64(pageSize) ||
		header[21] != 64 || header[22] != 32 || header[23] != 32 ||
		schemaFormat < 1 || schemaFormat > 4 || encoding < 1 || encoding > 3 {
		return fmt.Errorf("%w: malformed or unsupported SQLite header", ErrDatabaseUnsupported)
	}
	return nil
}

func validateModsSchema(ctx context.Context, db *sql.DB) (err error) {
	var objectType string
	if err := db.QueryRowContext(ctx, `SELECT type FROM pragma_table_list('mods') WHERE schema = 'main'`).Scan(&objectType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: mods table is missing: %w", ErrDatabaseUnsupported, err)
		}
		return err
	}
	if objectType != "table" {
		return fmt.Errorf("%w: mods must be an ordinary table, not %s", ErrDatabaseUnsupported, objectType)
	}
	rows, err := db.QueryContext(ctx, `PRAGMA main.table_info('mods')`)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()
	required := map[string]string{
		"id": "INTEGER", "name": "TEXT", "namespace": "TEXT", "filepath": "TEXT",
		"version": "TEXT", "websiteurl": "TEXT", "description": "TEXT", "frameworkid": "INTEGER",
	}
	implicitRowID := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, declaredType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &declaredType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		name = strings.ToLower(name)
		want, consumed := required[name]
		if !consumed {
			continue
		}
		rowID := name == "id" && primaryKey == 1 && strings.EqualFold(declaredType, "INTEGER")
		nullable := notNull == 0 && !rowID
		if columnAffinity(declaredType) != want ||
			(nullable && name != "websiteurl" && name != "description") {
			return fmt.Errorf("%w: incompatible mods column %q (%s)", ErrDatabaseUnsupported, name, declaredType)
		}
		implicitRowID = implicitRowID || (rowID && notNull == 0)
		delete(required, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(required) != 0 {
		return fmt.Errorf("%w: missing consumed mods columns", ErrDatabaseUnsupported)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if implicitRowID {
		// INTEGER PRIMARY KEY is implicitly non-null only when it aliases rowid,
		// not when SQLite creates an index for a composite or DESC primary key.
		var primaryIndexes int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_index_list('mods') WHERE origin = 'pk'`).Scan(&primaryIndexes); err != nil {
			return err
		}
		if primaryIndexes != 0 {
			return fmt.Errorf("%w: mods.id must be non-null", ErrDatabaseUnsupported)
		}
	}
	return nil
}

func columnAffinity(declaredType string) string {
	declaredType = strings.ToUpper(declaredType)
	switch {
	case strings.Contains(declaredType, "INT"):
		return "INTEGER"
	case strings.Contains(declaredType, "CHAR"), strings.Contains(declaredType, "CLOB"), strings.Contains(declaredType, "TEXT"):
		return "TEXT"
	default:
		return ""
	}
}

func classifyDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	class := ErrDatabaseUnavailable
	var sqliteErr sqlite3.Error
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		class = ErrDatabaseCancelled
	case errors.Is(err, ErrDatabaseUnsupported), errors.Is(err, ErrDatabaseUnavailable),
		errors.Is(err, ErrDatabaseBusy), errors.Is(err, ErrDatabaseRecoveryRequired), errors.Is(err, ErrDatabaseCancelled):
		return err
	case errors.As(err, &sqliteErr):
		switch {
		case sqliteErr.ExtendedCode == sqlite3.ErrReadonlyRollback || sqliteErr.ExtendedCode == sqlite3.ErrReadonlyRecovery:
			class = ErrDatabaseRecoveryRequired
		case sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked:
			class = ErrDatabaseBusy
		case sqliteErr.Code == sqlite3.ErrInterrupt:
			class = ErrDatabaseCancelled
		case sqliteErr.Code == sqlite3.ErrError || sqliteErr.Code == sqlite3.ErrNotADB ||
			sqliteErr.Code == sqlite3.ErrCorrupt || sqliteErr.Code == sqlite3.ErrSchema ||
			sqliteErr.Code == sqlite3.ErrFormat || sqliteErr.Code == sqlite3.ErrMismatch:
			class = ErrDatabaseUnsupported
		}
	}
	return fmt.Errorf("%w: %w", class, err)
}
