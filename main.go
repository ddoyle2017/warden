package main

import (
	"log"
	"warden/command"
	"warden/data"
	"warden/data/repo"
)

func main() {
	// Boot strap the app and initialize dependencies
	// Set up API clients

	// Open database and initialize tables if they don't already exist
	db, err := data.OpenDatabase()
	if err != nil {
		log.Fatal(err.Error())
	}
	data.CreateModsTable()

	// Initialize and injection dependencies into commands
	modsRepo := repo.NewModsRepo(db)
	listCmd := command.NewListCommand(modsRepo)
	addCmd := command.NewAddCommand(modsRepo)

	command.Execute(listCmd, addCmd)
}
