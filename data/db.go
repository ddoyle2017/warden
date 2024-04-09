package data

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

// Database is an interface for basic SQL driver functions that Warden needs. Its fulfilled by both the
// SQLite database driver and mock.Database
type Database interface {
	Query(query string, args ...any) (*sql.Rows, error)
	Prepare(query string) (*sql.Stmt, error)
}

func OpenDatabase() (Database, error) {
	var err error

	// probably need to inject this
	db, err = sql.Open("sqlite3", "./sqlite-database.db")
	if err != nil {
		return nil, err
	}
	return db, db.Ping()
}

func CreateModsTable() {
	modTableSQL := `CREATE TABLE IF NOT EXISTS mods (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"name" TEXT NOT NULL,
		"namespace" TEXT NOT NULL,
		"filePath" TEXT NOT NULL,
		"version" TEXT NOT NULL,
		"websiteUrl" TEXT,
		"description" TEXT
	  );`
	createTable(modTableSQL)
}

// func CreateModDependenciesTable() {
// 	modTableSQL := `CREATE TABLE IF NOT EXISTS mod_dependencies (
// 		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
// 		"name" TEXT NOT NULL,
// 		"filePath" TEXT NOT NULL,
// 		"version" TEXT NOT NULL,
// 		"websiteUrl" TEXT,
// 		"description" TEXT,
// 	  );`
// 	createTable("mod_dependencies", modTableSQL)
// }

func createTable(query string) {
	statement, err := db.Prepare(query)
	if err != nil {
		log.Fatal(err.Error())
	}

	statement.Exec()
}
