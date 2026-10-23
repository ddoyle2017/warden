package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/domain/framework"
	"path/filepath"
	"strings"
)

var (
	ErrFrameworkInsertFailed = errors.New("unable to insert new record into frameworks table")
	ErrFrameworkUpdateFailed = errors.New("unable to update record in frameworks table")
	ErrFrameworkDeleteFailed = errors.New("unable to delete frame")

	ErrFrameworkFetchFailed          = errors.New("unable to fetch specified framework from frameworks table")
	ErrFrameworkMappingFailed        = errors.New("unable to map framework record to struct")
	ErrFrameworkFetchNoResults       = errors.New("fetch query returned no results for specified framework")
	ErrFrameworkFetchMultipleResults = errors.New("fetch query returned multiple results for specified framework")
)

type Frameworks interface {
	GetFramework(name string) (framework.Framework, error)
	InsertFramework(f framework.Framework) error
	UpdateFramework(f framework.Framework) error
	// The caller must have completed file removal; plugin records are scoped to pluginDirectory.
	DeleteFramework(name, pluginDirectory string) error
}

type frameworks struct {
	db Database
}

func NewFrameworksRepo(db Database) Frameworks {
	return &frameworks{
		db: db,
	}
}

func (fr *frameworks) GetFramework(name string) (framework.Framework, error) {
	rows, err := fr.db.Query(`SELECT * FROM frameworks WHERE name = ?`, name)
	if err != nil {
		return framework.Framework{}, fmt.Errorf("%w: %w", ErrFrameworkFetchFailed, err)
	}
	defer rows.Close()

	frameworks, err := mapRowsToFramework(rows)
	if err != nil {
		return framework.Framework{}, fmt.Errorf("%w: %w", ErrFrameworkMappingFailed, err)
	}
	if len(frameworks) == 0 {
		return framework.Framework{}, ErrFrameworkFetchNoResults
	}
	if len(frameworks) > 1 {
		return framework.Framework{}, ErrFrameworkFetchMultipleResults
	}

	return frameworks[0], nil
}

func (fr *frameworks) InsertFramework(f framework.Framework) error {
	sql := `INSERT INTO frameworks(name, namespace, version, websiteUrl, description) VALUES (?, ?, ?, ?, ?)`

	return executeWrite(fr.db, sql, ErrFrameworkInsertFailed, f.Name, f.Namespace, f.Version, f.WebsiteURL, f.Description)
}

func (fr *frameworks) UpdateFramework(f framework.Framework) error {
	sql := `UPDATE frameworks 
	SET name = ?, namespace = ?, version = ?, websiteUrl = ?, description = ?
	WHERE id = ?`

	return executeWrite(fr.db, sql, ErrFrameworkUpdateFailed, f.Name, f.Namespace, f.Version, f.WebsiteURL, f.Description, f.ID)
}

func (fr *frameworks) DeleteFramework(name, pluginDirectory string) error {
	if pluginDirectory == "" {
		return fmt.Errorf("%w: plugin directory is required", ErrFrameworkDeleteFailed)
	}
	directory, err := filepath.Abs(pluginDirectory)
	if err != nil {
		return fmt.Errorf("%w: resolve plugin directory: %w", ErrFrameworkDeleteFailed, err)
	}

	return executeTransaction(fr.db, func(tx *sql.Tx) error {
		ids, err := modIDsInDirectory(tx, directory)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := executeStatement(tx, `DELETE FROM mods WHERE id = ?`, ErrModDeleteFailed, id); err != nil {
				return err
			}
		}
		return executeStatement(tx, `DELETE FROM frameworks WHERE name = ?`, ErrFrameworkDeleteFailed, name)
	})
}

func modIDsInDirectory(tx *sql.Tx, directory string) (ids []int, err error) {
	statement, err := tx.Prepare(`SELECT id, filePath FROM mods`)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidStatement, err)
	}
	defer func() {
		if closeErr := statement.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close mod paths statement: %w", closeErr))
		}
	}()
	rows, err := statement.Query()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrModListFailed, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close mod paths: %w", closeErr))
		}
	}()

	for rows.Next() {
		var id int
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrModMappingFailed, err)
		}
		// An empty legacy path cannot establish which files belonged to the record.
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("%w: resolve mod path %q: %w", ErrModMappingFailed, path, err)
		}
		relative, err := filepath.Rel(directory, absolute)
		if err != nil {
			return nil, fmt.Errorf("%w: compare mod path %q: %w", ErrModMappingFailed, path, err)
		}
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrModListFailed, err)
	}
	return ids, nil
}

func mapRowsToFramework(rows *sql.Rows) ([]framework.Framework, error) {
	frameworks := []framework.Framework{}

	for rows.Next() {
		var id int
		var name string
		var namespace string
		var version string
		var url string
		var description string

		err := rows.Scan(&id, &name, &namespace, &version, &url, &description)
		if err != nil {
			return []framework.Framework{}, err
		}

		f := framework.Framework{
			ID:          id,
			Name:        name,
			Namespace:   namespace,
			Version:     version,
			WebsiteURL:  url,
			Description: description,
		}
		frameworks = append(frameworks, f)
	}
	return frameworks, rows.Err()
}
