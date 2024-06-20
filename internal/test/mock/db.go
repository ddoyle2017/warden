package mock

import "database/sql"

// Database implements the data.Database interface and exposes anonymous member functions for mocking
// data.Database behavior
type Database struct {
	QueryFunc   func(query string, args ...any) (*sql.Rows, error)
	PrepareFunc func(query string) (*sql.Stmt, error)
	BeginFunc   func() (*sql.Tx, error)
}

func (d *Database) Query(query string, args ...any) (*sql.Rows, error) {
	return d.QueryFunc(query, args...)
}

func (d *Database) Prepare(query string) (*sql.Stmt, error) {
	return d.PrepareFunc(query)
}

func (d *Database) Begin() (*sql.Tx, error) {
	return d.BeginFunc()
}
