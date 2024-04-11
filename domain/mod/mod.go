package mod

import "slices"

type Mod struct {
	ID           int
	Name         string
	Namespace    string
	FilePath     string
	Version      string
	WebsiteURL   string
	Description  string
	Dependencies []string
}

func (m1 *Mod) Equals(m2 *Mod) bool {
	return m1.ID == m2.ID &&
		m1.Name == m2.Name &&
		m1.Namespace == m2.Namespace &&
		m1.FilePath == m2.FilePath &&
		m1.Version == m2.Version &&
		m1.WebsiteURL == m2.WebsiteURL &&
		m1.Description == m2.Description &&
		slices.Equal(m1.Dependencies, m2.Dependencies)
}
