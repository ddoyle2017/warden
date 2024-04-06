package mod

type Mod struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	FilePath     string   `json:"file_path"`
	Version      string   `json:"version"`
	WebsiteURL   string   `json:"website_url"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies"`
}
