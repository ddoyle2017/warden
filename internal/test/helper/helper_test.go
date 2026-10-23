package helper_test

import (
	"errors"
	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"github.com/ddoyle2017/warden/internal/test/helper"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDatabaseIsolation(t *testing.T) {
	t.Run("given two helpers, when deleting one database, then preserve the other database", func(t *testing.T) {
		first := helper.NewHelper(t)
		firstDB := first.CreateDatabase()
		repo.CreateModsTable(firstDB)
		repo.CreateFrameworksTable(firstDB)
		first.SeedModsTable(repo.NewModsRepo(firstDB), repo.NewFrameworksRepo(firstDB))

		second := helper.NewHelper(t)
		secondDB := second.CreateDatabase()
		repo.CreateModsTable(secondDB)
		repo.CreateFrameworksTable(secondDB)
		secondMods := repo.NewModsRepo(secondDB)

		mods, err := secondMods.ListMods()
		if err != nil {
			t.Fatal(err)
		}
		if len(mods) != 0 {
			t.Fatalf("expected an independent empty database, received %+v", mods)
		}

		first.DeleteDatabase()
		expected := second.SeedModsTable(secondMods, repo.NewFrameworksRepo(secondDB))
		mods, err = secondMods.ListMods()
		if err != nil {
			t.Fatal(err)
		}
		if !slices.EqualFunc(mods, expected, func(a, b mod.Mod) bool {
			return a.Equals(&b)
		}) {
			t.Errorf("expected second database to remain writable with mods %+v, received %+v", expected, mods)
		}
	})
}

func TestServerFileIsolation(t *testing.T) {
	t.Run("given two helpers, when removing one server tree, then preserve the other tree", func(t *testing.T) {
		first := helper.NewHelper(t)
		second := helper.NewHelper(t)
		first.SetUpServerFiles()
		second.SetUpServerFiles()
		expected, err := second.GetFileList(second.GetValheimDirectory())
		if err != nil {
			t.Fatal(err)
		}
		if len(expected) == 0 {
			t.Fatal("expected populated server fixtures")
		}

		first.RemoveServerFiles()

		remaining, err := first.GetFileList(first.GetValheimDirectory())
		if err != nil {
			t.Fatal(err)
		}
		if len(remaining) != 0 {
			t.Errorf("expected first server tree to be empty, received %+v", remaining)
		}
		actual, err := second.GetFileList(second.GetValheimDirectory())
		if err != nil {
			t.Fatal(err)
		}
		if !second.AreFileListsEqual(expected, actual) {
			t.Errorf("expected second server files %+v, received %+v", expected, actual)
		}
	})
}

func TestHelperCleanup(t *testing.T) {
	var directory string
	var db repo.Database

	if !t.Run("when a helper's test finishes, then release its files and database", func(t *testing.T) {
		th := helper.NewHelper(t)
		directory = th.GetValheimDirectory()
		if err := os.WriteFile(filepath.Join(directory, "server.txt"), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
		db = th.CreateDatabase()
	}) {
		t.Fatal("helper setup failed")
	}

	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected temporary server directory to be removed, received error %v", err)
	}
	rows, err := db.Query("SELECT 1")
	if err == nil {
		if closeErr := rows.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Error("expected the database connection to be closed")
	}
}
