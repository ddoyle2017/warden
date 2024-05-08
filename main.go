package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"warden/command"
	"warden/internal/api/thunderstore"
	"warden/internal/config"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/service"

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
	db, err := repo.OpenDatabase(filepath.Join(home, ".warden.db"))
	if err != nil {
		log.Fatal(err.Error())
	}
	repo.CreateModsTable(db)
	repo.CreateFrameworksTable(db)

	// Initialize and injection dependencies into commands
	mr := repo.NewModsRepo(db)
	fr := repo.NewFrameworksRepo(db)
	ts := thunderstore.New(&http.Client{})
	fm := file.NewManager(&http.Client{}, cfg.ValheimDirectory)

	ms := service.NewModService(mr, fm, ts, os.Stdin)
	fs := service.NewFrameworkService(fr, fm, ts, os.Stdin)

	// Register commands
	listCmd := command.NewListCommand(ms)
	addCmd := command.NewAddCommand(fs, ms)
	removeCmd := command.NewRemoveCommand(fs, ms)
	updateCmd := command.NewUpdateCommand(fs, ms)
	configCmd := command.NewConfigCommand(*cfg)

	command.Execute(listCmd, addCmd, removeCmd, updateCmd, configCmd)
}
