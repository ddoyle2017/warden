package command

import (
	"fmt"
	"strings"
	"warden/api/thunderstore"
	"warden/data/file"
	"warden/data/repo"
	"warden/domain/mod"
)

func addDependencies(r repo.Mods, fm file.Manager, ts thunderstore.Thunderstore, dependencies []string) {
	// If mod has dependencies, install them
	if len(dependencies) != 0 {
		fmt.Println("... installing mod dependencies ...")

		for _, dep := range dependencies {
			details := strings.Split(dep, "-")

			namespace, name := details[0], details[1]

			_, err := ts.GetPackage(namespace, name)
			if err != nil {
				fmt.Println("... error fetching dependencies ...")
				return
			}
			err = r.DeleteMod(name, namespace)
			if err != nil {
				fmt.Println("... error removing previous installation of dependency ...")
				return
			}
		}
	}
}

func updateMod(r repo.Mods, fm file.Manager, currentFullName string, latest thunderstore.Release) {
	// Delete the previous installation before installing new version
	err := fm.RemoveMod(currentFullName)
	if err != nil {
		fmt.Println("... unable to remove current version ...")
		return
	}
	addMod(r, fm, latest)
	fmt.Println("... mod successfully updated! ...")
}

func addMod(r repo.Mods, fm file.Manager, release thunderstore.Release) {
	// Download and install the mod files
	path, err := fm.InstallMod(release.DownloadURL, release.FullName)
	if err != nil {
		fmt.Println("... failed to install mod ...")
		r.DeleteMod(release.Name, release.Namespace)
		return
	}

	m := mod.Mod{
		Name:         release.Name,
		Namespace:    release.Namespace,
		FilePath:     path,
		Version:      release.VersionNumber,
		WebsiteURL:   release.WebsiteURL,
		Description:  release.Description,
		Dependencies: release.Dependencies,
	}
	err = r.UpsertMod(m)
	if err != nil {
		fmt.Println("... failed to save mod ...")
	}
}
