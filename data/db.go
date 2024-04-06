package data

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

type Database interface {
	Query(query string, args ...any) (*sql.Rows, error)
	Prepare(query string) (*sql.Stmt, error)
}

func OpenDatabase() (Database, error) {
	var err error

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
		"filePath" TEXT NOT NULL,
		"version" TEXT NOT NULL,
		"websiteUrl" TEXT,
		"description" TEXT
	  );`
	createTable("mods", modTableSQL)
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

func createTable(name, query string) {
	statement, err := db.Prepare(query)
	if err != nil {
		log.Fatal(err.Error())
	}

	statement.Exec()
	log.Printf("...%s table created...", name)
}
