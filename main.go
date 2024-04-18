package main

import (
	"log"
	"net/http"
	"os"
	"warden/api/thunderstore"
	"warden/command"
	"warden/data"
	"warden/data/file"
	"warden/data/repo"
	"warden/service"
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
	r := repo.NewModsRepo(db)
	ts := thunderstore.New(&http.Client{})
	fm := file.NewManager("./test/file", &http.Client{})

	modService := service.NewModService(r, fm, ts, os.Stdin)

	listCmd := command.NewListCommand(modService)
	addCmd := command.NewAddCommand(modService)
	removeCmd := command.NewRemoveCommand(r, fm)
	updateCmd := command.NewUpdateCommand(modService)

	command.Execute(listCmd, addCmd, removeCmd, updateCmd)
}
