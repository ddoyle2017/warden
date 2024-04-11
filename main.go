package main

import (
	"log"
	"net/http"
	"warden/api/thunderstore"
	"warden/command"
	"warden/data"
	"warden/data/file"
	"warden/data/repo"
)

func main() {
	// Boot strap the app and initialize dependencies
	// Set up API clients

	// Open database and initialize tables if they don't already exist
	db, err := data.OpenDatabase("./sqlite-database.db")
	if err != nil {
		log.Fatal(err.Error())
	}
	data.CreateModsTable(db)

	// Initialize and injection dependencies into commands
	modsRepo := repo.NewModsRepo(db)
	ts := thunderstore.New(&http.Client{})
	manager := file.NewManager("./test/file", &http.Client{})

	listCmd := command.NewListCommand(modsRepo)
	addCmd := command.NewAddCommand(modsRepo, ts, manager)
	removeCmd := command.NewRemoveCommand(modsRepo, manager)

	command.Execute(listCmd, addCmd, removeCmd)
}
