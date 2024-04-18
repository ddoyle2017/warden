package command

import (
	"errors"
	"strings"
	"warden/api/thunderstore"
	"warden/data/file"
	"warden/data/repo"
	"warden/domain/mod"
)

var (
	ErrAddModFailed          = errors.New("unable to add and install mod")
	ErrUpdateModFailed       = errors.New("unable to update existing mod")
	ErrAddDependenciesFailed = errors.New("unable to install mod's dependencies")
)

func addDependencies(r repo.Mods, fm file.Manager, ts thunderstore.Thunderstore, dependencies []string) error {
	for _, dep := range dependencies {
		details := strings.Split(dep, "-")

		namespace, name := details[0], details[1]

		pkg, err := ts.GetPackage(namespace, name)
		if err != nil {
			return ErrAddDependenciesFailed
		}

		err = r.DeleteMod(name, namespace)
		if err != nil {
			return ErrAddDependenciesFailed
		}

		addMod(r, fm, pkg.Latest)
	}
	return nil
}

func updateMod(r repo.Mods, fm file.Manager, currentFullName string, latest thunderstore.Release) error {
	// Delete the previous installation before installing new version
	err := fm.RemoveMod(currentFullName)
	if err != nil {
		return ErrUpdateModFailed
	}
	return addMod(r, fm, latest)
}

func addMod(r repo.Mods, fm file.Manager, release thunderstore.Release) error {
	// Download and install the mod files
	path, err := fm.InstallMod(release.DownloadURL, release.FullName)
	if err != nil {
		r.DeleteMod(release.Name, release.Namespace)
		return ErrAddModFailed
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
		return ErrAddModFailed
	}
	return nil
}
