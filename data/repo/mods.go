package repo

import (
	"database/sql"
	"errors"
	"log"
	"warden/data"
	"warden/domain/mod"
)

var (
	ErrModListFailed      = errors.New("unable to return list of records from mods table")
	ErrModInsertFailed    = errors.New("unable to insert new record into mods table")
	ErrModDeleteFailed    = errors.New("unable to delete record from mods table")
	ErrModDeleteAllFailed = errors.New("unable to remove all records from mods table")

	ErrModFetchFailed          = errors.New("unable to fetch specified mod from mods table")
	ErrModFetchNoResults       = errors.New("fetch query returned no results for specified mod")
	ErrModFetchMultipleResults = errors.New("fetch query reurned multiple results for specified mod")

	ErrModMappingFailed = errors.New("unable to map mods records to mod struct slice")
)

type Mods interface {
	ListMods() ([]mod.Mod, error)
	GetMod(name string) (mod.Mod, error)
	InsertMod(m mod.Mod) error
	DeleteMod(modName, namespace string) error
	DeleteAllMods() error
}

type repo struct {
	db data.Database
}

func NewModsRepo(db data.Database) Mods {
	return &repo{
		db: db,
	}
}

func (r *repo) ListMods() ([]mod.Mod, error) {
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

func (r *repo) GetMod(name string) (mod.Mod, error) {
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

func (r *repo) InsertMod(m mod.Mod) error {
	sql := `INSERT INTO mods(name, namespace, filePath, version, websiteUrl, description) VALUES (?, ?, ?, ?, ?, ?)`

	statement, err := r.db.Prepare(sql)
	if err != nil {
		log.Fatalln(err)
	}

	_, err = statement.Exec(m.Name, m.Namespace, m.FilePath, m.Version, m.WebsiteURL, m.Description)
	if err != nil {
		log.Fatalln(err)
		return err
	}
	return nil
}

func (r *repo) DeleteMod(modName, namespace string) error {
	sql := `DELETE FROM mods WHERE name = ? AND namespace = ?`

	statement, err := r.db.Prepare(sql)
	if err != nil {
		log.Fatalln(err)
	}

	_, err = statement.Exec(modName, namespace)
	if err != nil {
		log.Fatalln(err)
		return err
	}
	return nil
}

func (r *repo) DeleteAllMods() error {
	// Whole table delete instead of dropping the table because its not guaranteed the user will recreate the table
	// on their next command
	sql := `DELETE FROM mods WHERE id IS NOT NULL`

	statement, err := r.db.Prepare(sql)
	if err != nil {
		log.Fatalln(err)
	}

	_, err = statement.Exec()
	if err != nil {
		log.Fatalln(err)
		return err
	}
	return nil
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

		err := rows.Scan(&id, &name, &namespace, &path, &version, &url, &description)
		if err != nil {
			return []mod.Mod{}, err
		}

		m := mod.Mod{
			ID:          id,
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
