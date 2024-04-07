package thunderstore

// Package is the top level definition of a mod. It contains data about the mod, its different releases, user ratings, etc..
type Package struct {
	Namespace         string    `json:"namespace"`
	Name              string    `json:"name"`
	FullName          string    `json:"full_name"`
	Owner             string    `json:"owner"` // also called 'Namespace'
	PackageURL        string    `json:"package_url"`
	DateCreated       string    `json:"date_created"`
	DateUpdated       string    `json:"date_updated"`
	RatingScore       int       `json:"rating_score"`
	IsPinned          bool      `json:"is_pinned"`
	IsDeprecated      bool      `json:"is_deprecated"`
	TotalDownloads    int64     `json:"total_downloads"`
	Latest            Release   `json:"latest"`
	CommunityListings []Listing `json:"community_listings"`
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
	DateCreated   string   `json:"date_created"`
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
