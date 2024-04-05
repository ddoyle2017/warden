package mod

// Mod contains metadata about a mod. Based on the standard Valheim mod's manifest.json
type Mod struct {
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	WebsiteUrl    string   `json:"website_url"`
	Description   string   `json:"description"`
	Dependencies  []string `json:"dependencies"`
}
