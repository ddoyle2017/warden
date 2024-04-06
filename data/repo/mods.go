package repo

import (
	"log"
	"warden/data"
	"warden/domain/mod"
)

type Mods interface {
	ListMods() []mod.Mod
	InsertMod(m mod.Mod) error
}

type repo struct {
	db data.Database
}

func NewModsRepo(db data.Database) Mods {
	return &repo{
		db: db,
	}
}

func (r *repo) ListMods() []mod.Mod {
	rows, err := r.db.Query(`SELECT * FROM mods`)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer rows.Close()

	mods := []mod.Mod{}
	for rows.Next() {
		var id int
		var name string
		var path string
		var version string
		var url string
		var description string

		rows.Scan(&id, &name, &path, &version, &url, &description)
		m := mod.Mod{
			ID:          id,
			Name:        name,
			FilePath:    path,
			Version:     version,
			WebsiteURL:  url,
			Description: description,
		}
		mods = append(mods, m)
	}
	return mods
}

func (r *repo) InsertMod(m mod.Mod) error {
	sql := `INSERT INTO mods(name, filePath, version, websiteUrl, description) VALUES (?, ?, ?, ?, ?)`

	statement, err := r.db.Prepare(sql)
	if err != nil {
		log.Fatalln(err)
	}

	_, err = statement.Exec(m.Name, m.FilePath, m.Version, m.WebsiteURL, m.Description)
	if err != nil {
		log.Fatalln(err)
		return err
	}
	return nil
}
