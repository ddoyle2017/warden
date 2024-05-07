package framework

/*
A Framework is a special kind of mod that enables the development
of plugins. Frameworks expose tools and code that other mod authors
build with. Because of this, each framework has specific rules around
how its installed and they are handled separately from normal mods.

Examples include:
  - BepInEx (Valheim, Skyrim, etc..)
  - Optifine (closed source Minecraft framework)
  - Fabric (open source Minecraft framework)
  - and more!
*/
const (
	BepInExNamespace = "denikson"
	BepInEx          = "BepInExPack_Valheim"
)

type Framework struct {
	ID          int
	Name        string
	Namespace   string
	Version     string
	WebsiteURL  string
	Description string
}

func (f1 *Framework) Equals(f2 *Framework) bool {
	return f1.ID == f2.ID &&
		f1.Name == f2.Name &&
		f1.Namespace == f2.Namespace &&
		f1.Version == f2.Version &&
		f1.WebsiteURL == f2.WebsiteURL &&
		f1.Description == f2.Description
}

func (f *Framework) FullName() string {
	return f.Namespace + "-" + f.Name + "-" + f.Version
}
