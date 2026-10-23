package helper

import (
	"database/sql"
	"errors"
	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/internal/domain/framework"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"os"
	"path/filepath"
)

type databaseHelper interface {
	CreateDatabase() repo.Database
	DeleteDatabase()

	SeedModsTable(mr repo.Mods, fr repo.Frameworks) []mod.Mod
	SeedFrameworksTable(fr repo.Frameworks) []framework.Framework
}

func (h *helper) CreateDatabase() repo.Database {
	h.t.Helper()
	path := filepath.Join(h.databaseFolder, h.databaseFile)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		h.t.Fatalf("unexpected error when creating test database, received: %+v", err)
	}
	h.databases = append(h.databases, db)
	if err := db.Ping(); err != nil {
		h.t.Fatalf("unexpected error when opening test database, received: %+v", err)
	}
	return db
}

func (h *helper) DeleteDatabase() {
	h.t.Helper()
	for _, db := range h.databases {
		if err := db.Close(); err != nil {
			h.t.Errorf("unexpected error when closing test database, received: %+v", err)
		}
	}
	h.databases = nil

	err := os.Remove(filepath.Join(h.databaseFolder, h.databaseFile))
	if errors.Is(err, os.ErrNotExist) {
		// Test database was already removed. This is fine, so we ignore the error and continue.
		return
	}
	if err != nil {
		h.t.Errorf("unexpected error when cleaning up test database, received error: %+v", err)
	}
}

func (h *helper) SeedModsTable(mr repo.Mods, fr repo.Frameworks) []mod.Mod {
	h.t.Helper()
	h.SeedFrameworksTable(fr)

	mods := []mod.Mod{
		{
			ID:          1,
			FrameworkID: 1,
			Namespace:   "Azumatt",
			Name:        "Sleepover",
		},
		{
			ID:          2,
			FrameworkID: 1,
			Namespace:   "Azumatt",
			Name:        "Where_You_At",
		},
		{
			ID:          3,
			FrameworkID: 1,
			Namespace:   "Azumatt",
			Name:        "AzuClock",
		},
	}

	for _, m := range mods {
		err := mr.InsertMod(m)
		if err != nil {
			h.t.Fatalf("unexpected error when seeding test database, received: %+v", err)
		}
	}
	return mods
}

func (h *helper) SeedFrameworksTable(fr repo.Frameworks) []framework.Framework {
	h.t.Helper()
	frameworks := []framework.Framework{
		{
			ID:          1,
			Namespace:   "denikson",
			Name:        "BepInExPack_Valheim",
			Version:     "5.4.2202",
			WebsiteURL:  "https://github.com/BepInEx/BepInEx",
			Description: "BepInEx pack for Valheim. Preconfigured and includes unstripped Unity DLLs.",
		},
	}

	for _, f := range frameworks {
		err := fr.InsertFramework(f)
		if err != nil {
			h.t.Fatalf("unexpected error when seeding test database, received: %+v", err)
		}
	}
	return frameworks
}
