package mod

import "slices"

// A Mod is a single plugin or library that modifies the behavior of a game. This
// can be anything from gameplay changes, to new settings, to new content, and etc.
type Mod struct {
	ID           int
	FrameworkID  int
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
		m1.FrameworkID == m2.FrameworkID &&
		m1.Name == m2.Name &&
		m1.Namespace == m2.Namespace &&
		m1.FilePath == m2.FilePath &&
		m1.Version == m2.Version &&
		m1.WebsiteURL == m2.WebsiteURL &&
		m1.Description == m2.Description &&
		slices.Equal(m1.Dependencies, m2.Dependencies)
}

func (m *Mod) FullName() string {
	return m.Namespace + "-" + m.Name + "-" + m.Version
}
