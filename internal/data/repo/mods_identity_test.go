package repo_test

import (
	"database/sql"
	"errors"
	"slices"
	"testing"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestUpsertMod_NamespacedIdentity(t *testing.T) {
	for name, existingSecond := range map[string]bool{
		"given one author, when installing another author's same-name mod, then retain both records":      false,
		"given two authors, when updating one same-name mod, then preserve both IDs and the other record": true,
	} {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			repo.CreateModsTable(db)
			th.SeedFrameworksTable(repo.NewFrameworksRepo(db))
			mr := repo.NewModsRepo(db)
			first := mod.Mod{
				ID: 1, FrameworkID: 1, Namespace: "AuthorA", Name: "Shared",
				FilePath: "plugins/AuthorA-Shared-1.0.0", Version: "1.0.0",
				WebsiteURL: "https://example.invalid/a", Description: "first author's mod",
			}
			second := mod.Mod{
				ID: 2, FrameworkID: 1, Namespace: "AuthorB", Name: "Shared",
				FilePath: "plugins/AuthorB-Shared-1.0.0", Version: "1.0.0",
				WebsiteURL: "https://example.invalid/b", Description: "second author's mod",
			}
			if err := mr.InsertMod(first); err != nil {
				t.Fatal(err)
			}
			if existingSecond {
				if err := mr.InsertMod(second); err != nil {
					t.Fatal(err)
				}
			}

			// Reopen the existing database rather than initializing a replacement schema.
			mr = repo.NewModsRepo(th.CreateDatabase())
			update := second
			if existingSecond {
				update = first
				update.Version = "2.0.0"
				update.FilePath = "plugins/AuthorA-Shared-2.0.0"
				update.WebsiteURL = "https://example.invalid/a/latest"
				update.Description = "updated first author's mod"
				first = update
			}
			update.ID = 0
			if err := mr.UpsertMod(update); err != nil {
				t.Fatal(err)
			}

			got, err := mr.ListMods()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 {
				t.Errorf("expected exactly two records, got %+v", got)
			}
			for _, want := range []mod.Mod{first, second} {
				if !slices.ContainsFunc(got, func(m mod.Mod) bool { return m.Equals(&want) }) {
					t.Errorf("missing unchanged identity and metadata %+v in %+v", want, got)
				}
			}
		})
	}
}

func TestGetMod_NamespacedIdentity(t *testing.T) {
	for name, tt := range map[string]struct {
		namespace string
		nameOnly  bool
		wantID    int
		wantErr   error
	}{
		"given two authors, when selecting the first namespace, then return its full record": {
			namespace: "AuthorA", wantID: 1,
		},
		"given two authors, when selecting the second namespace, then return its full record": {
			namespace: "AuthorB", wantID: 2,
		},
		"given two authors, when selecting another namespace, then do not fall back to a name match": {
			namespace: "Missing", wantErr: repo.ErrModFetchNoResults,
		},
		"given two authors, when selecting only by name, then reject the ambiguity": {
			nameOnly: true, wantErr: repo.ErrModFetchMultipleResults,
		},
		"given an empty explicit namespace, when selecting, then do not fall back to a name match": {
			wantErr: repo.ErrModFetchNoResults,
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := helper.NewHelper(t).CreateDatabase()
			repo.CreateModsTable(db)
			mr := repo.NewModsRepo(db)
			records := []mod.Mod{
				{ID: 1, Namespace: "AuthorA", Name: "Shared", Version: "1.0.0", FilePath: "first", WebsiteURL: "first-url", Description: "first"},
				{ID: 2, Namespace: "AuthorB", Name: "Shared", Version: "2.0.0", FilePath: "second", WebsiteURL: "second-url", Description: "second"},
			}
			for _, m := range records {
				if err := mr.InsertMod(m); err != nil {
					t.Fatal(err)
				}
			}
			var got mod.Mod
			var err error
			if tt.nameOnly {
				got, err = mr.GetMod("Shared")
			} else {
				got, err = mr.GetModByIdentity(tt.namespace, "Shared")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
			want := mod.Mod{}
			if tt.wantID != 0 {
				want = records[tt.wantID-1]
			}
			if !got.Equals(&want) {
				t.Errorf("expected %+v, got %+v", want, got)
			}
		})
	}
}

func TestUpsertMod_RejectsFailedLookup(t *testing.T) {
	for name, tt := range map[string]struct {
		query     string
		duplicate bool
		wantErr   error
	}{
		"given a query failure, when upserting, then do not attempt a write": {
			wantErr: repo.ErrModFetchFailed,
		},
		"given invalid stored metadata, when upserting, then do not interpret it as absence": {
			query:   `SELECT id, name, namespace, filePath, version, NULL, description, frameworkId FROM mods`,
			wantErr: repo.ErrModMappingFailed,
		},
		"given a row iteration failure after one match, when upserting, then do not select the partial result": {
			query: `SELECT id, name, namespace, filePath, version, websiteUrl, description, frameworkId FROM mods
							UNION ALL SELECT abs(-9223372036854775808), name, namespace, filePath, version, websiteUrl, description, frameworkId FROM mods`,
			wantErr: repo.ErrModMappingFailed,
		},
		"given duplicate records for one namespace and name, when upserting, then retain both without inserting another": {
			duplicate: true, wantErr: repo.ErrModFetchMultipleResults,
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := helper.NewHelper(t).CreateDatabase()
			repo.CreateModsTable(db)
			mr := repo.NewModsRepo(db)
			original := mod.Mod{ID: 1, Namespace: "AuthorA", Name: "Shared", Version: "1.0.0", FilePath: "original", Description: "original metadata"}
			if err := mr.InsertMod(original); err != nil {
				t.Fatal(err)
			}
			want := []mod.Mod{original}
			if tt.duplicate {
				duplicate := original
				duplicate.ID = 2
				duplicate.Version = "0.9.0"
				if err := mr.InsertMod(duplicate); err != nil {
					t.Fatal(err)
				}
				want = append(want, duplicate)
			}
			faulted := repo.NewModsRepo(&mock.Database{
				QueryFunc: func(query string, args ...any) (*sql.Rows, error) {
					if tt.duplicate {
						return db.Query(query, args...)
					}
					if tt.query != "" {
						return db.Query(tt.query)
					}
					return nil, sql.ErrConnDone
				},
				PrepareFunc: func(string) (*sql.Stmt, error) {
					t.Fatal("lookup failure must not prepare a write")
					return nil, sql.ErrConnDone
				},
				BeginFunc: func() (*sql.Tx, error) {
					t.Fatal("lookup failure must not begin a write")
					return nil, sql.ErrConnDone
				},
			})
			update := original
			update.Version = "2.0.0"
			if err := faulted.UpsertMod(update); !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
			got, err := mr.ListMods()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want) {
				t.Errorf("failed lookup changed record count: want %+v, got %+v", want, got)
			}
			for _, m := range want {
				if !slices.ContainsFunc(got, func(record mod.Mod) bool { return record.Equals(&m) }) {
					t.Errorf("failed lookup changed metadata: want %+v in %+v", m, got)
				}
			}
		})
	}
}
