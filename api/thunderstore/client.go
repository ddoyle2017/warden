package thunderstore

// Interface for Thunderstore's API for Valheim mods. See docs: https://thunderstore.io/c/valheim/create/docs/
type Thunderstore interface {
	GetPackage()
}

type api struct {
}

func (a *api) GetPackage() {

}
