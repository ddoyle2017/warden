package mod_test

import (
	"testing"
	"warden/domain/mod"
)

func TestEquals_Happy(t *testing.T) {
	modA := mod.Mod{
		ID:           1,
		Name:         "Sleepover",
		Namespace:    "Azumatt",
		FilePath:     "/file/path/test",
		Version:      "1.0.1",
		WebsiteURL:   "www.google.com/some-file",
		Description:  "A mod for sleepovers",
		Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
	}
	modB := mod.Mod{
		ID:           1,
		Name:         "Sleepover",
		Namespace:    "Azumatt",
		FilePath:     "/file/path/test",
		Version:      "1.0.1",
		WebsiteURL:   "www.google.com/some-file",
		Description:  "A mod for sleepovers",
		Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
	}

	if !modA.Equals(&modB) {
		t.Errorf("expected true, received false")
	}
}

func TestEquals_Sad(t *testing.T) {
	modA := mod.Mod{
		ID:           1,
		Name:         "Sleepover",
		Namespace:    "Azumatt",
		FilePath:     "/file/path/test",
		Version:      "1.0.1",
		WebsiteURL:   "www.google.com/some-file",
		Description:  "A mod for sleepovers",
		Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
	}
	modB := mod.Mod{
		ID:           2,
		Name:         "Where_You_At",
		Namespace:    "Azumatt",
		FilePath:     "/file/path/test",
		Version:      "1.0.16",
		WebsiteURL:   "www.google.com/some-other-file",
		Description:  "A mod forcing player location on map",
		Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
	}

	if modA.Equals(&modB) {
		t.Errorf("expected false, received true")
	}
}

func TestFullName(t *testing.T) {
	expected := "Azumatt-Sleepover-1.0.1"
	mod := mod.Mod{
		ID:           1,
		Name:         "Sleepover",
		Namespace:    "Azumatt",
		FilePath:     "/file/path/test",
		Version:      "1.0.1",
		WebsiteURL:   "www.google.com/some-file",
		Description:  "A mod for sleepovers",
		Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
	}

	if mod.FullName() != expected {
		t.Errorf("expected mod fullname to be: %s, received: %s", expected, mod.FullName())
	}
}
