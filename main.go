package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"warden/api/thunderstore"
	"warden/command"
	"warden/config"
	"warden/data"
	"warden/data/file"
	"warden/data/repo"
	"warden/service"

	"github.com/mitchellh/go-homedir"
)

func main() {
	// Load in the config
	home, err := homedir.Dir()
	if err != nil {
		log.Fatal(err)
	}

	cfg, err := config.Load(home)
	if err != nil {
		log.Fatal(err.Error())
	}

	// Open database and initialize tables if they don't already exist
	db, err := data.OpenDatabase("./sqlite-database.db")
	if err != nil {
		log.Fatal(err.Error())
	}
	data.CreateModsTable(db)

	// Initialize and injection dependencies into commands
	md := filepath.Join(cfg.ValheimDirectory, cfg.ModDirectory)

	r := repo.NewModsRepo(db)
	ts := thunderstore.New(&http.Client{})
	fm := file.NewManager(md, &http.Client{})

	modService := service.NewModService(r, fm, ts, os.Stdin)

	listCmd := command.NewListCommand(modService)
	addCmd := command.NewAddCommand(modService)
	removeCmd := command.NewRemoveCommand(modService)
	updateCmd := command.NewUpdateCommand(modService)
	configCmd := command.NewConfigCommand(*cfg)

	command.Execute(listCmd, addCmd, removeCmd, updateCmd, configCmd)
}
