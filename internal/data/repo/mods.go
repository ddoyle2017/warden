package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ddoyle2017/warden/internal/domain/mod"
)

var (
	ErrInvalidStatement  = errors.New("SQL statement is invalid or incorrectly formatted")
	ErrTransactionFailed = errors.New("unable to start a SQL transaction")

	ErrModListFailed      = errors.New("unable to return list of records from mods table")
	ErrModInsertFailed    = errors.New("unable to insert new record into mods table")
	ErrModUpdateFailed    = errors.New("unable to update record in mods table")
	ErrModDeleteFailed    = errors.New("unable to delete record from mods table")
	ErrModDeleteAllFailed = errors.New("unable to remove all records from mods table")

	ErrModFetchFailed          = errors.New("unable to fetch specified mod from mods table")
	ErrModFetchNoResults       = errors.New("fetch query returned no results for specified mod")
	ErrModFetchMultipleResults = errors.New("fetch query reurned multiple results for specified mod")

	ErrModMappingFailed = errors.New("unable to map mod record to mod struct")
)

type Mods interface {
	ListMods() ([]mod.Mod, error)
	ListModsContext(ctx context.Context) ([]mod.Mod, error)
	GetMod(name string) (mod.Mod, error)
	GetModByIdentity(namespace, name string) (mod.Mod, error)
	InsertMod(m mod.Mod) error
	UpdateMod(m mod.Mod) error
	UpsertMod(m mod.Mod) error
	DeleteMod(modName, namespace string) error
	DeleteAllMods() error
}

type mods struct {
	db Database
}

func NewModsRepo(db Database) Mods {
	return &mods{
		db: db,
	}
}

func (r *mods) ListMods() ([]mod.Mod, error) {
	return r.ListModsContext(context.Background())
}

func (r *mods) ListModsContext(ctx context.Context) ([]mod.Mod, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, namespace, filePath, version, websiteUrl, description, frameworkId FROM mods`)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrModListFailed, classifyDatabaseError(err))
	}

	mods, err := mapRowsToMod(rows)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrModMappingFailed, classifyDatabaseError(err))
	}
	return mods, nil
}

func (r *mods) GetModByIdentity(namespace, name string) (mod.Mod, error) {
	return r.getMod(`SELECT id, name, namespace, filePath, version, websiteUrl, description, frameworkId
		FROM mods WHERE namespace = ? AND name = ?`, namespace, name)
}

func (r *mods) GetMod(name string) (mod.Mod, error) {
	return r.getMod(`SELECT id, name, namespace, filePath, version, websiteUrl, description, frameworkId
		FROM mods WHERE name = ?`, name)
}

func (r *mods) getMod(query string, args ...any) (mod.Mod, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return mod.Mod{}, fmt.Errorf("%w: %w", ErrModFetchFailed, err)
	}
	mods, err := mapRowsToMod(rows)
	if err != nil {
		return mod.Mod{}, fmt.Errorf("%w: %w", ErrModMappingFailed, err)
	}
	if len(mods) == 0 {
		return mod.Mod{}, ErrModFetchNoResults
	}
	if len(mods) > 1 {
		return mod.Mod{}, ErrModFetchMultipleResults
	}

	return mods[0], nil
}

func (r *mods) InsertMod(m mod.Mod) error {
	sql := `INSERT INTO mods(name, namespace, filePath, version, websiteUrl, description, frameworkId) VALUES (?, ?, ?, ?, ?, ?, ?)`

	return executeWrite(r.db, sql, ErrModInsertFailed, m.Name, m.Namespace, m.FilePath, m.Version, m.WebsiteURL, m.Description, m.FrameworkID)
}

func (r *mods) UpdateMod(m mod.Mod) error {
	sql := `UPDATE mods 
			SET name = ?, namespace = ?, filePath = ?, version = ?, websiteUrl = ?, description = ?
			WHERE id = ?`

	return executeWrite(r.db, sql, ErrModUpdateFailed, m.Name, m.Namespace, m.FilePath, m.Version, m.WebsiteURL, m.Description, m.ID)
}

func (r *mods) UpsertMod(m mod.Mod) error {
	current, err := r.GetModByIdentity(m.Namespace, m.Name)
	if errors.Is(err, ErrModFetchNoResults) {
		return r.InsertMod(m)
	}
	if err != nil {
		return err
	}
	m.ID = current.ID
	return r.UpdateMod(m)
}

func (r *mods) DeleteMod(modName, namespace string) error {
	sql := `DELETE FROM mods WHERE name = ? AND namespace = ?`

	return executeWrite(r.db, sql, ErrModDeleteFailed, modName, namespace)
}

func (r *mods) DeleteAllMods() error {
	// Whole table delete instead of dropping the table because its not guaranteed the user will recreate the table
	// on their next command
	sql := `DELETE FROM mods WHERE id IS NOT NULL`

	return executeWrite(r.db, sql, ErrModDeleteAllFailed)
}

type modRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

func mapRowsToMod(rows modRows) (mods []mod.Mod, err error) {
	defer func() {
		err = errors.Join(err, rows.Close())
		if err != nil {
			mods = nil
		}
	}()
	mods = []mod.Mod{}
	for rows.Next() {
		var id int
		var name string
		var namespace string
		var path string
		var version string
		var url sql.NullString
		var description sql.NullString
		var frameworkId int

		err := rows.Scan(&id, &name, &namespace, &path, &version, &url, &description, &frameworkId)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %w", ErrDatabaseUnsupported, err)
		}

		m := mod.Mod{
			ID:          id,
			FrameworkID: frameworkId,
			Name:        name,
			Namespace:   namespace,
			FilePath:    path,
			Version:     version,
			WebsiteURL:  url.String,
			Description: description.String,
		}
		mods = append(mods, m)
	}
	return mods, rows.Err()
}
