package repo

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

// Database is an interface for basic SQL driver functions that Warden needs. Its fulfilled by both the
// SQLite database driver and mock.Database
type Database interface {
	// Executes the given SQL query, using any passed in args
	Query(query string, args ...any) (*sql.Rows, error)

	// Prepares and executes a SQL statement (UPDATE, INSERT, etc.)
	Prepare(query string) (*sql.Stmt, error)

	// Begins a SQL transaction
	Begin() (*sql.Tx, error)
}

func OpenDatabase(dbFile string) (Database, error) {
	var err error

	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		return nil, err
	}
	return db, db.Ping()
}

func CreateModsTable(db Database) {
	modsTableSQL := `CREATE TABLE IF NOT EXISTS mods (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"name" TEXT NOT NULL,
		"namespace" TEXT NOT NULL,
		"filePath" TEXT NOT NULL,
		"version" TEXT NOT NULL,
		"websiteUrl" TEXT,
		"description" TEXT,
		"frameworkId" INTEGER NOT NULL, 
		FOREIGN KEY (frameworkId) REFERENCES frameworks(id)
	  );`
	createTable(db, modsTableSQL)
}

func CreateFrameworksTable(db Database) {
	frameworksTableSQL := `CREATE TABLE IF NOT EXISTS frameworks (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"name" TEXT NOT NULL,
		"namespace" TEXT NOT NULL,
		"version" TEXT NOT NULL,
		"websiteUrl" TEXT,
		"description" TEXT
	  );`
	createTable(db, frameworksTableSQL)
}

func createTable(db Database, query string) {
	statement, err := db.Prepare(query)
	if err != nil {
		log.Fatal(err.Error())
	}
	statement.Exec()
}
