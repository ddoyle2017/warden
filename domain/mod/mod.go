package mod

type Mod struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Namespace    string   `json:"name_space"`
	FilePath     string   `json:"file_path"`
	Version      string   `json:"version"`
	WebsiteURL   string   `json:"website_url"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies"`
}
