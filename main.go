package main

import (
	"log"
	"net/http"
	"os"
	"warden/api/thunderstore"
	"warden/command"
	"warden/config"
	"warden/data"
	"warden/data/file"
	"warden/data/repo"
	"warden/service"
)

func main() {
	// Load in the config
	cfg := config.New(
		config.WithModDirectory("./test/files"),
	)
	if err := cfg.LoadConfig("."); err != nil {
		log.Fatal(err.Error())
	}

	// Open database and initialize tables if they don't already exist
	db, err := data.OpenDatabase("./sqlite-database.db")
	if err != nil {
		log.Fatal(err.Error())
	}
	data.CreateModsTable(db)

	// Initialize and injection dependencies into commands
	r := repo.NewModsRepo(db)
	ts := thunderstore.New(&http.Client{})
	fm := file.NewManager(cfg.ModDirectory, &http.Client{})

	modService := service.NewModService(r, fm, ts, os.Stdin)

	listCmd := command.NewListCommand(modService)
	addCmd := command.NewAddCommand(modService)
	removeCmd := command.NewRemoveCommand(modService)
	updateCmd := command.NewUpdateCommand(modService)

	command.Execute(listCmd, addCmd, removeCmd, updateCmd)
}
