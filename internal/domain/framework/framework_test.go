package framework_test

import (
	"testing"
	"warden/internal/domain/framework"
)

func TestEquals_Happy(t *testing.T) {
	frameworkA := framework.Framework{
		ID:          1,
		Name:        framework.BepInEx,
		Namespace:   framework.BepInExNamespace,
		Version:     "1.0.0",
		WebsiteURL:  "github.com/some-framework",
		Description: "BepInEx is a modding framework for Valheim",
	}
	frameworkB := framework.Framework{
		ID:          1,
		Name:        framework.BepInEx,
		Namespace:   framework.BepInExNamespace,
		Version:     "1.0.0",
		WebsiteURL:  "github.com/some-framework",
		Description: "BepInEx is a modding framework for Valheim",
	}

	if !frameworkA.Equals(&frameworkB) {
		t.Error("expected true, received false")
	}
}

func TestEquals_Sad(t *testing.T) {
	frameworkA := framework.Framework{
		ID:          1,
		Name:        framework.BepInEx,
		Namespace:   framework.BepInExNamespace,
		Version:     "1.0.0",
		WebsiteURL:  "github.com/some-framework",
		Description: "BepInEx is a modding framework for Valheim",
	}
	frameworkB := framework.Framework{
		ID:          2,
		Name:        "FabricPack_Valheim",
		Namespace:   "fabric",
		Version:     "1.0.0",
		WebsiteURL:  "github.com/some-framework",
		Description: "Fabric is an API for developing fake Valheim mods",
	}

	if frameworkA.Equals(&frameworkB) {
		t.Error("expected false, received true")
	}
}

func TestEquals_FullName(t *testing.T) {
	fr := framework.Framework{
		ID:          1,
		Name:        framework.BepInEx,
		Namespace:   framework.BepInExNamespace,
		Version:     "1.0.0",
		WebsiteURL:  "github.com/some-framework",
		Description: "BepInEx is a modding framework for Valheim",
	}
	expected := framework.BepInExNamespace + "-" + framework.BepInEx + "-" + fr.Version

	if fr.FullName() != expected {
		t.Errorf("expected framework to have fullname: %s, received: %s", expected, fr.FullName())
	}
}
