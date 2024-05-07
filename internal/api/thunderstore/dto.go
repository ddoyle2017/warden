package thunderstore

import "slices"

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

func (p *Package) Equals(p2 *Package) bool {
	return p.Namespace == p2.Namespace &&
		p.Name == p2.Name &&
		p.FullName == p2.FullName &&
		p.Owner == p2.Owner &&
		p.PackageURL == p2.PackageURL &&
		p.DateCreated == p2.DateCreated &&
		p.DateUpdated == p2.DateUpdated &&
		p.RatingScore == p2.RatingScore &&
		p.IsPinned == p2.IsPinned &&
		p.IsDeprecated == p2.IsDeprecated &&
		p.TotalDownloads == p2.TotalDownloads &&
		p.Latest.Equals(&p2.Latest) &&
		slices.EqualFunc(p.CommunityListings, p2.CommunityListings, func(a, b Listing) bool {
			return a.Equals(&b)
		})
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

func (r *Release) Equals(r2 *Release) bool {
	return r.Namespace == r2.Namespace &&
		r.Name == r2.Name &&
		r.VersionNumber == r2.VersionNumber &&
		r.FullName == r2.FullName &&
		r.Description == r2.Description &&
		r.Icon == r2.Icon &&
		slices.Equal(r.Dependencies, r2.Dependencies) &&
		r.DownloadURL == r2.DownloadURL &&
		r.Downloads == r2.Downloads &&
		r.DateCreated == r2.DateCreated &&
		r.WebsiteURL == r2.WebsiteURL &&
		r.IsActive == r2.IsActive
}

// Listing is a collection of community metadata about a package, e.g. what game patches it works for, whether its NSFW, etc..
type Listing struct {
	HasNSFWContent bool     `json:"has_nsfw_content"`
	Categories     []string `json:"categories"`
	Community      string   `json:"community"` // AKA the game
	ReviewStatus   string   `json:"review_status"`
}

func (l *Listing) Equals(l2 *Listing) bool {
	return l.HasNSFWContent == l2.HasNSFWContent &&
		slices.Equal(l.Categories, l2.Categories) &&
		l.Community == l2.Community &&
		l.ReviewStatus == l2.ReviewStatus
}

type ErrorResponse struct {
	Detail string `json:"detail"`
}
