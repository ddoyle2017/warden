package repo

import (
	"log"
	"warden/data"
	"warden/domain/mod"
)

type Mods interface {
	ListMods() []mod.Mod
	InsertMod(m mod.Mod) error
	DeleteMod(modName, namespace string) error
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
		var namespace string
		var path string
		var version string
		var url string
		var description string

		rows.Scan(&id, &name, &namespace, &path, &version, &url, &description)
		m := mod.Mod{
			ID:          id,
			Name:        name,
			Namespace:   namespace,
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
	sql := `INSERT INTO mods(name, namespace, filePath, version, websiteUrl, description) VALUES (?, ?, ?, ?, ?, ?)`

	statement, err := r.db.Prepare(sql)
	if err != nil {
		log.Fatalln(err)
	}

	_, err = statement.Exec(m.Name, m.Namespace, m.FilePath, m.Version, m.WebsiteURL, m.Description)
	if err != nil {
		log.Fatalln(err)
		return err
	}
	return nil
}

func (r *repo) DeleteMod(modName, namespace string) error {
	sql := `DELETE FROM mods WHERE name = ? AND namespace = ?`

	statement, err := r.db.Prepare(sql)
	if err != nil {
		log.Fatalln(err)
	}

	_, err = statement.Exec(modName, namespace)
	if err != nil {
		log.Fatalln(err)
		return err
	}
	return nil
}
