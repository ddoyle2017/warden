package thunderstore

// Package is the top level definition of a mod. It contains data about the mod, its different releases, user ratings, etc..
type Package struct {
	Namespace         string
	Name              string
	FullName          string
	Owner             string // also called 'Namespace'
	PackageURL        string
	DateCreated       string // change to datetime type later
	DateUpdated       string // change to datetime type later
	RatingScore       int
	IsPinned          bool
	IsDeprecated      bool
	TotalDownloads    int64
	Latest            Release
	CommunityListings []Listing
}

// Release is a specific, released version of a Package.
type Release struct {
	Namespace     string   `json:"namespace"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	FullName      string   `json:"full_name"`
	Description   string   `json:"description"`
	Icon          string   `json:"icon"`
	Dependencies  []string `json:"dependencies"`
	DownloadURL   string   `json:"download_url"`
	Downloads     int64    `json:"downloads"`
	DateCreated   string   `json:"date_created"` // change to datetime type later
	WebsiteURL    string   `json:"website_url"`
	IsActive      bool     `json:"is_active"`
}

// Listing is a collection of community metadata about a package, e.g. what game patches it works for, whether its NSFW, etc..
type Listing struct {
	HasNSFWContent bool     `json:"has_nsfw_content"`
	Categories     []string `json:"categories"`
	Community      string   `json:"community"` // AKA the game
	ReviewStatus   string   `json:"review_status"`
}
