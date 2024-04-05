package mod

type Inventory interface {
	List() []Mod
	Total() int
}

type inventory struct {
	mods []Mod
}

func NewInventory() Inventory {
	return &inventory{
		mods: []Mod{},
	}
}

func (i *inventory) List() []Mod {
	return i.mods
}

func (i *inventory) Total() int {
	return len(i.mods)
}
