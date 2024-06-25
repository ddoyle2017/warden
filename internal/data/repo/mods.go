package repo

import (
	"database/sql"
	"errors"
	"warden/internal/domain/mod"
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
	GetMod(name string) (mod.Mod, error)
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
	rows, err := r.db.Query(`SELECT * FROM mods`)
	if err != nil {
		return []mod.Mod{}, ErrModListFailed
	}
	defer rows.Close()

	mods, err := mapRowsToMod(rows)
	if err != nil {
		return []mod.Mod{}, ErrModMappingFailed
	}
	return mods, nil
}

func (r *mods) GetMod(name string) (mod.Mod, error) {
	rows, err := r.db.Query(`SELECT * FROM mods WHERE name = ?`, name)
	if err != nil {
		return mod.Mod{}, ErrModFetchFailed
	}
	defer rows.Close()

	mods, err := mapRowsToMod(rows)
	if err != nil {
		return mod.Mod{}, ErrModMappingFailed
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

	tx, err := r.db.Begin()
	if err != nil {
		return ErrTransactionFailed
	}
	statement, err := tx.Prepare(sql)
	if err != nil {
		tx.Rollback()
		return ErrInvalidStatement
	}
	defer statement.Close()

	_, err = statement.Exec(m.Name, m.Namespace, m.FilePath, m.Version, m.WebsiteURL, m.Description, m.FrameworkID)
	if err != nil {
		tx.Rollback()
		return ErrModInsertFailed
	}
	return tx.Commit()
}

func (r *mods) UpdateMod(m mod.Mod) error {
	sql := `UPDATE mods 
			SET name = ?, namespace = ?, filePath = ?, version = ?, websiteUrl = ?, description = ?
			WHERE id = ?`

	tx, err := r.db.Begin()
	if err != nil {
		return ErrTransactionFailed
	}

	statement, err := r.db.Prepare(sql)
	if err != nil {
		tx.Rollback()
		return ErrInvalidStatement
	}
	defer statement.Close()

	_, err = statement.Exec(m.Name, m.Namespace, m.FilePath, m.Version, m.WebsiteURL, m.Description, m.ID)
	if err != nil {
		tx.Rollback()
		return ErrModUpdateFailed
	}
	return tx.Commit()
}

func (r *mods) UpsertMod(m mod.Mod) error {
	current, err := r.GetMod(m.Name)
	// If mod doesn't exist, insert new. If it does exist, update it
	if errors.Is(err, sql.ErrNoRows) || current.Equals(&mod.Mod{}) {
		return r.InsertMod(m)
	} else if err == nil {
		m.ID = current.ID
		return r.UpdateMod(m)
	} else {
		return err
	}
}

func (r *mods) DeleteMod(modName, namespace string) error {
	sql := `DELETE FROM mods WHERE name = ? AND namespace = ?`

	tx, err := r.db.Begin()
	if err != nil {
		return ErrTransactionFailed
	}

	statement, err := r.db.Prepare(sql)
	if err != nil {
		tx.Rollback()
		return ErrInvalidStatement
	}
	defer statement.Close()

	_, err = statement.Exec(modName, namespace)
	if err != nil {
		tx.Rollback()
		return ErrModDeleteFailed
	}
	return tx.Commit()
}

func (r *mods) DeleteAllMods() error {
	// Whole table delete instead of dropping the table because its not guaranteed the user will recreate the table
	// on their next command
	sql := `DELETE FROM mods WHERE id IS NOT NULL`

	tx, err := r.db.Begin()
	if err != nil {
		return ErrTransactionFailed
	}

	statement, err := r.db.Prepare(sql)
	if err != nil {
		tx.Rollback()
		return ErrInvalidStatement
	}
	defer statement.Close()

	_, err = statement.Exec()
	if err != nil {
		tx.Rollback()
		return ErrModDeleteAllFailed
	}
	return tx.Commit()
}

func mapRowsToMod(rows *sql.Rows) ([]mod.Mod, error) {
	mods := []mod.Mod{}
	for rows.Next() {
		var id int
		var name string
		var namespace string
		var path string
		var version string
		var url string
		var description string
		var frameworkId int

		err := rows.Scan(&id, &name, &namespace, &path, &version, &url, &description, &frameworkId)
		if err != nil {
			return []mod.Mod{}, err
		}

		m := mod.Mod{
			ID:          id,
			FrameworkID: frameworkId,
			Name:        name,
			Namespace:   namespace,
			FilePath:    path,
			Version:     version,
			WebsiteURL:  url,
			Description: description,
		}
		mods = append(mods, m)
	}
	return mods, nil
}
