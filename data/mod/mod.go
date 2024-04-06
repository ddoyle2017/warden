package mod

import (
	"log"
	"warden/data"
)

type Mod struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	FilePath     string   `json:"file_path"`
	Version      string   `json:"version"`
	WebsiteURL   string   `json:"website_url"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies"`
}

type ModsRepo interface {
	ListMods() []Mod
	InsertMod(m Mod) error
}

type repo struct {
	db data.Database
}

func NewRepo(db data.Database) ModsRepo {
	return &repo{
		db: db,
	}
}

func (r *repo) ListMods() []Mod {
	rows, err := r.db.Query(`SELECT * FROM mods`)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer rows.Close()

	mods := []Mod{}
	for rows.Next() {
		var id int
		var name string
		var path string
		var version string
		var url string
		var description string

		rows.Scan(&id, &name, &path, &version, &url, &description)
		m := Mod{
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

func (r *repo) InsertMod(m Mod) error {
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
