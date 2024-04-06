package mod

type Inventory interface {
	Add(name string) (bool, error)
	Remove(name string) (bool, error)
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

func (i *inventory) Add(name string) (bool, error) {
	return true, nil
}

func (i *inventory) Remove(name string) (bool, error) {
	return true, nil
}

func (i *inventory) List() []Mod {
	return i.mods
}

func (i *inventory) Total() int {
	return len(i.mods)
}
