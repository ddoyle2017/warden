package repo

import (
	"database/sql"
	"errors"
	"warden/internal/domain/framework"
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
	DeleteFramework(name string) error
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
		return framework.Framework{}, ErrFrameworkFetchFailed
	}
	defer rows.Close()

	frameworks, err := mapRowsToFramework(rows)
	if err != nil {
		return framework.Framework{}, ErrFrameworkMappingFailed
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

	tx, err := fr.db.Begin()
	if err != nil {
		return ErrTransactionFailed
	}

	statement, err := fr.db.Prepare(sql)
	if err != nil {
		tx.Rollback()
		return ErrInvalidStatement
	}
	defer statement.Close()

	_, err = statement.Exec(f.Name, f.Namespace, f.Version, f.WebsiteURL, f.Description)
	if err != nil {
		tx.Rollback()
		return ErrFrameworkInsertFailed
	}
	return tx.Commit()
}

func (fr *frameworks) UpdateFramework(f framework.Framework) error {
	sql := `UPDATE frameworks 
	SET name = ?, namespace = ?, version = ?, websiteUrl = ?, description = ?
	WHERE id = ?`

	tx, err := fr.db.Begin()
	if err != nil {
		return ErrTransactionFailed
	}

	statement, err := fr.db.Prepare(sql)
	if err != nil {
		tx.Rollback()
		return ErrInvalidStatement
	}
	defer statement.Close()

	_, err = statement.Exec(f.Name, f.Namespace, f.Version, f.WebsiteURL, f.Description, f.ID)
	if err != nil {
		tx.Rollback()
		return ErrFrameworkUpdateFailed
	}
	return tx.Commit()
}

func (fr *frameworks) DeleteFramework(name string) error {
	sql := `DELETE FROM frameworks WHERE name = ?`

	tx, err := fr.db.Begin()
	if err != nil {
		return ErrTransactionFailed
	}

	statement, err := fr.db.Prepare(sql)
	if err != nil {
		tx.Rollback()
		return ErrInvalidStatement
	}

	_, err = statement.Exec(name)
	if err != nil {
		tx.Rollback()
		return ErrFrameworkDeleteFailed
	}
	return tx.Commit()
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
	return frameworks, nil
}
