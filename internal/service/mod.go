package service

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/domain/mod"
)

var (
	ErrUnableToListMods    = errors.New("unable to list mods")
	ErrUnableToRemoveMod   = errors.New("unable to remove mod")
	ErrUnableToUpdateMod   = errors.New("unable to update mod")
	ErrModNotInstalled     = errors.New("mod not installed")
	ErrModNameAmbiguous    = errors.New("multiple installed mods have this name")
	ErrDuplicateModRecords = errors.New("duplicate installation records")

	ErrModAlreadyInstalled = errors.New("mod is already installed")
	ErrModInstallFailed    = errors.New("unable to install new mod")
	ErrModNotFound         = errors.New("mod not found")

	ErrAddDependenciesFailed = errors.New("unable to install mod's dependencies")
)

// ModMutationCleanupError marks a completed add/update request, including metadata
// and direct dependencies, with cleanup warnings. Only a direct return establishes
// whole-request completion; nested file/removal markers do not.
type ModMutationCleanupError struct {
	Err error
}

func (e *ModMutationCleanupError) Error() string {
	return fmt.Sprintf("mod request completed; cleanup failed: %v", e.Err)
}

func (e *ModMutationCleanupError) Unwrap() error {
	return e.Err
}

// Encapsulates all the business logic for managing mods. It coordinates both the mods
// database and file management to make sure they're updated together.
type Mod interface {
	ListMods() ([]mod.Mod, error)
	AddMod(namespace, name string) error
	UpdateMod(name string) error
	UpdateModByIdentity(namespace, name string) error
	UpdateAllMods() error
	RemoveMod(namespace, name string) error
	RemoveAllMods() error
}

type modService struct {
	r  repo.Mods
	fm file.Manager
	ts thunderstore.Thunderstore
	in *bufio.Scanner
}

func NewModService(r repo.Mods, fm file.Manager, ts thunderstore.Thunderstore, reader io.Reader) Mod {
	return &modService{
		r:  r,
		fm: fm,
		ts: ts,
		in: bufio.NewScanner(reader),
	}
}

func (ms *modService) ListMods() ([]mod.Mod, error) {
	return ms.r.ListMods()
}

func (ms *modService) AddMod(namespace, name string) error {
	current, err := ms.r.GetModByIdentity(namespace, name)
	if err != nil && !errors.Is(err, repo.ErrModFetchNoResults) {
		return fmt.Errorf("%w: %w", ErrModInstallFailed, err)
	}
	installed := err == nil

	var release thunderstore.Release
	if installed {
		if err := validateReleaseVersion("installed", current.Version); err != nil {
			return fmt.Errorf("%w: %s/%s: %w", ErrAddDependenciesFailed, namespace, name, err)
		}
		release, err = ms.ts.GetRelease(current.Namespace, current.Name, current.Version)
		if err != nil {
			return fmt.Errorf("%w: cannot determine dependencies for installed %s; root left unchanged; retry when its exact-version metadata is available: %w",
				ErrAddDependenciesFailed, current.FullName(), err)
		}
	} else {
		pkg, err := ms.ts.GetPackage(namespace, name)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrModNotFound, err)
		}
		release = pkg.Latest
	}

	dependencies, err := planDependencies(release)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAddDependenciesFailed, err)
	}

	var cleanupErr error
	if !installed {
		cleanupErr, err = ms.installMod(release)
		if err != nil {
			return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", ErrModInstallFailed, err))
		}
	}
	missing, err := ms.missingDependencies(dependencies)
	if err != nil {
		return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", ErrAddDependenciesFailed, err))
	}
	if installed && len(missing) == 0 {
		return ErrModAlreadyInstalled
	}

	if len(missing) > 0 {
		if installed {
			fmt.Printf("... keeping installed %s; repairing %d missing direct dependencies ...\n", current.FullName(), len(missing))
		} else {
			fmt.Printf("... mod has %d missing direct dependencies, installing them ...\n", len(missing))
		}

		dependencyCleanup, err := ms.addDependencies(missing)
		cleanupErr = errors.Join(cleanupErr, dependencyCleanup)
		if err != nil {
			return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", ErrAddDependenciesFailed, err))
		}
	}
	return modMutationResult(cleanupErr, nil)
}

func (ms *modService) missingDependencies(dependencies []dependency) ([]dependency, error) {
	missing := make([]dependency, 0, len(dependencies))
	seen := make(map[dependency]bool, len(dependencies))
	for _, dep := range dependencies {
		if dep.name == framework.BepInEx || seen[dep] {
			continue
		}
		seen[dep] = true
		_, err := ms.r.GetModByIdentity(dep.namespace, dep.name)
		if errors.Is(err, repo.ErrModFetchNoResults) {
			missing = append(missing, dep)
		} else if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", dep.namespace, dep.name, err)
		}
	}
	return missing, nil
}

func (ms *modService) UpdateMod(name string) error {
	current, err := ms.r.GetMod(name)
	if errors.Is(err, repo.ErrModFetchMultipleResults) {
		return ms.describeModAmbiguity(name, err)
	}
	return ms.updateInstalledMod(current, err)
}

func (ms *modService) UpdateModByIdentity(namespace, name string) error {
	current, err := ms.r.GetModByIdentity(namespace, name)
	if errors.Is(err, repo.ErrModFetchMultipleResults) {
		return fmt.Errorf("%w: %w for %s/%s; inspect installation records before retrying: %w",
			ErrUnableToUpdateMod, ErrDuplicateModRecords, namespace, name, err)
	}
	return ms.updateInstalledMod(current, err)
}

func (ms *modService) describeModAmbiguity(name string, lookupErr error) error {
	mods, err := ms.r.ListMods()
	if err != nil {
		return fmt.Errorf("%w: %w; unable to list matching installations: %w: %w",
			ErrUnableToUpdateMod, ErrModNameAmbiguous, err, lookupErr)
	}
	counts := make(map[string]int)
	matches := 0
	for _, m := range mods {
		if m.Name == name {
			counts[m.Namespace]++
			matches++
		}
	}
	if matches < 2 {
		return fmt.Errorf("%w: %w; matching installation records changed during lookup; retry: %w",
			ErrUnableToUpdateMod, ErrModNameAmbiguous, lookupErr)
	}
	var identities, duplicates []string
	for namespace, count := range counts {
		identity := namespace + "/" + name
		identities = append(identities, identity)
		if count > 1 {
			duplicates = append(duplicates, identity)
		}
	}
	slices.Sort(identities)
	slices.Sort(duplicates)
	detail := "matching packages: " + strings.Join(identities, ", ")
	if len(identities) > 1 {
		detail += "; specify --namespace to select a package"
	}
	if len(duplicates) > 0 {
		return fmt.Errorf("%w: %w; %s; %w for %s; inspect installation records before retrying: %w",
			ErrUnableToUpdateMod, ErrModNameAmbiguous, detail, ErrDuplicateModRecords, strings.Join(duplicates, ", "), lookupErr)
	}
	return fmt.Errorf("%w: %w; %s: %w", ErrUnableToUpdateMod, ErrModNameAmbiguous, detail, lookupErr)
}

func (ms *modService) updateInstalledMod(current mod.Mod, err error) error {
	if err != nil && errors.Is(err, repo.ErrModFetchNoResults) {
		return ErrModNotInstalled
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnableToUpdateMod, err)
	}

	// Fetch the latest version from online
	pkg, err := ms.ts.GetPackage(current.Namespace, current.Name)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrModNotFound, err)
	}

	newer, err := hasNewerRelease(current.Version, pkg.Latest.VersionNumber)
	if err != nil {
		return fmt.Errorf("%w: %s/%s: %w", ErrUnableToUpdateMod, current.Namespace, current.Name, err)
	}
	if newer {
		fmt.Printf("... found a new version (%s) of %s %s ...\n", pkg.Latest.VersionNumber, current.Namespace, current.Name)
		fmt.Printf("did you want to update this mod? %s\n", yesOrNo)

		accepted, err := confirm(ms.in, yes, no)
		if err != nil {
			return err
		}
		if !accepted {
			fmt.Println("... aborting ...")
			return nil
		}
		dependencies, err := planDependencies(pkg.Latest)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrAddDependenciesFailed, err)
		}
		cleanupErr, err := ms.updateMod(current, pkg.Latest)
		if err != nil {
			return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", ErrUnableToUpdateMod, err))
		}
		dependencyCleanup, err := ms.addDependencies(dependencies)
		cleanupErr = errors.Join(cleanupErr, dependencyCleanup)
		if err != nil {
			return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", ErrAddDependenciesFailed, err))
		}
		return modMutationResult(cleanupErr, nil)
	} else {
		fmt.Printf("... latest version of %s %s already installed (%s) ...\n", current.Namespace, current.Name, current.Version)
	}
	return nil
}

func (ms *modService) UpdateAllMods() error {
	fmt.Printf("are you sure you wanted to update ALL mods? %s\n", yesOrNo)

	accepted, err := confirm(ms.in, yes, no)
	if err != nil {
		return err
	}
	if !accepted {
		fmt.Println("... aborting ...")
		return nil
	}
	// Get all installed mods
	mods, err := ms.r.ListMods()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnableToListMods, err)
	}
	// Count original records, but reject duplicates only when their package is reached.
	identityCounts := make(map[[2]string]int, len(mods))
	for _, m := range mods {
		identityCounts[[2]string{m.Namespace, m.Name}]++
	}
	validateIdentity := func(namespace, name string) error {
		if identityCounts[[2]string{namespace, name}] > 1 {
			return fmt.Errorf("%w: %w for %s/%s; inspect installation records before retrying: %w",
				ErrUnableToUpdateMod, ErrDuplicateModRecords, namespace, name, repo.ErrModFetchMultipleResults)
		}
		return nil
	}

	selected := make(map[[2]string]mod.Mod, len(mods))
	for _, m := range mods {
		selected[[2]string{m.Namespace, m.Name}] = m
	}
	updated := make(map[[2]string][]dependency)
	replace := func(current mod.Mod, latest thunderstore.Release) (cleanupErr, failure error) {
		if err := validateIdentity(current.Namespace, current.Name); err != nil {
			return nil, err
		}
		newer, err := hasNewerRelease(current.Version, latest.VersionNumber)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", current.Namespace, current.Name, err)
		}
		if !newer {
			return nil, nil
		}
		dependencies, err := planDependencies(latest)
		if err != nil {
			return nil, err
		}
		cleanupErr, failure = ms.updateMod(current, latest)
		if failure == nil {
			updated[[2]string{current.Namespace, current.Name}] = dependencies
		}
		return cleanupErr, failure
	}
	installDependency := func(latest thunderstore.Release) (cleanupErr, failure error) {
		identity := [2]string{latest.Namespace, latest.Name}
		if _, done := updated[identity]; done {
			return nil, nil
		}
		if current, installed := selected[identity]; installed {
			return replace(current, latest)
		}
		return ms.installMod(latest)
	}

	var cleanupErr error
	for _, m := range mods {
		if err := validateIdentity(m.Namespace, m.Name); err != nil {
			return modMutationResult(cleanupErr, err)
		}
		identity := [2]string{m.Namespace, m.Name}
		dependencies, alreadyUpdated := updated[identity]
		if !alreadyUpdated {
			pkg, err := ms.ts.GetPackage(m.Namespace, m.Name)
			if err != nil {
				return modMutationResult(cleanupErr, fmt.Errorf("%w: %s/%s: %w", ErrModNotFound, m.Namespace, m.Name, err))
			}

			updateCleanup, err := replace(m, pkg.Latest)
			cleanupErr = errors.Join(cleanupErr, updateCleanup)
			if err != nil {
				operationErr := ErrUnableToUpdateMod
				if errors.Is(err, ErrInvalidDependency) {
					operationErr = ErrAddDependenciesFailed
				}
				return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", operationErr, err))
			}
			dependencies, alreadyUpdated = updated[identity]
			if !alreadyUpdated {
				fmt.Printf("... latest version of %s %s already installed (%s) ...\n", m.Namespace, m.Name, m.Version)
				continue
			}
		}
		// A dependency may already be replaced, but its own selected turn still owns its direct dependencies.
		dependencyCleanup, err := ms.addDependenciesWithInstaller(dependencies, installDependency, validateIdentity)
		cleanupErr = errors.Join(cleanupErr, dependencyCleanup)
		if err != nil {
			return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", ErrAddDependenciesFailed, err))
		}
	}
	return modMutationResult(cleanupErr, nil)
}

func (ms *modService) RemoveMod(namespace, name string) error {
	fmt.Printf("are you sure you want to remove this mod? %s\n", yesOrNo)

	accepted, err := confirm(ms.in, yes, no)
	if err != nil {
		return err
	}
	if !accepted {
		fmt.Println("... aborting ...")
		return nil
	}
	// Find the current installation of the mod
	current, err := ms.r.GetModByIdentity(namespace, name)
	if err != nil && errors.Is(err, repo.ErrModFetchNoResults) {
		return ErrModNotInstalled
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnableToRemoveMod, err)
	}

	removal, err := ms.fm.StageModRemoval(current.FullName())
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnableToRemoveMod, err)
	}
	return ms.finishModRemoval(removal, func() error {
		return ms.r.DeleteMod(current.Name, current.Namespace)
	})
}

func (ms *modService) RemoveAllMods() error {
	fmt.Printf("are you sure you want to remove ALL mods? %s\n", yesOrNoLong)

	accepted, err := confirm(ms.in, yesLong, noLong)
	if err != nil {
		return err
	}
	if !accepted {
		fmt.Println("... aborting ...")
		return nil
	}
	removal, err := ms.fm.StageAllModsRemoval()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnableToRemoveMod, err)
	}
	return ms.finishModRemoval(removal, ms.r.DeleteAllMods)
}

func (ms *modService) finishModRemoval(removal file.ModRemoval, deleteRecords func() error) error {
	if err := deleteRecords(); err != nil {
		return fmt.Errorf("%w: record deletion failed; verify database state: %w", ErrUnableToRemoveMod, errors.Join(err, removal.Rollback()))
	}
	if err := removal.Commit(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnableToRemoveMod, err)
	}
	return nil
}

func (ms *modService) addDependencies(dependencies []dependency) (cleanupErr, failure error) {
	return ms.addDependenciesWithInstaller(dependencies, ms.installMod, nil)
}

func (ms *modService) addDependenciesWithInstaller(dependencies []dependency, install func(thunderstore.Release) (error, error), validateIdentity func(string, string) error) (cleanupErr, failure error) {
	for _, dep := range dependencies {
		// If dep is BepInEx, skip
		if dep.name == framework.BepInEx {
			continue
		}
		if validateIdentity != nil {
			if err := validateIdentity(dep.namespace, dep.name); err != nil {
				return cleanupErr, err
			}
		}

		pkg, err := ms.ts.GetPackage(dep.namespace, dep.name)
		if err != nil {
			return cleanupErr, fmt.Errorf("%s/%s: %w", dep.namespace, dep.name, err)
		}
		installCleanup, err := install(pkg.Latest)
		cleanupErr = errors.Join(cleanupErr, installCleanup)
		if err != nil {
			return cleanupErr, err
		}
	}
	return cleanupErr, nil
}

func (ms *modService) updateMod(current mod.Mod, latest thunderstore.Release) (cleanupErr, failure error) {
	if err := ms.fm.ValidateModInstall(); err != nil {
		return nil, err
	}

	replacement, err := ms.fm.ReplaceMod(latest.DownloadURL, current.FullName(), latest.FullName)
	if err != nil {
		if _, completed := err.(*file.ModInstallCleanupError); !completed {
			return nil, fmt.Errorf("%s: %w", latest.FullName, err)
		}
		cleanupErr = fmt.Errorf("%s: %w", latest.FullName, err)
	}
	if err := ms.recordMod(latest, replacement.Path()); err != nil {
		recovery := "original files were absent"
		if path := replacement.RecoveryPath(); path != "" {
			recovery = fmt.Sprintf("original files retained at %q", path)
		}
		return cleanupErr, fmt.Errorf("%w; %s; original record before replacement: %+v", err, recovery, current)
	}
	if err := replacement.Commit(); err != nil {
		if _, completed := err.(*file.ModInstallCleanupError); !completed {
			return cleanupErr, fmt.Errorf("%s: finalize replacement: %w", latest.FullName, err)
		}
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("%s: %w", latest.FullName, err))
	}
	return cleanupErr, nil
}

func (ms *modService) installMod(release thunderstore.Release) (cleanupErr, failure error) {
	// Download and install the mod files
	path, err := ms.fm.InstallMod(release.DownloadURL, release.FullName)
	if err != nil {
		if _, completed := err.(*file.ModInstallCleanupError); !completed {
			return nil, fmt.Errorf("%s: %w", release.FullName, err)
		}
		cleanupErr = fmt.Errorf("%s: %w", release.FullName, err)
	}
	return cleanupErr, ms.recordMod(release, path)
}

func (ms *modService) recordMod(release thunderstore.Release, path string) error {
	m := mod.Mod{
		Name:         release.Name,
		Namespace:    release.Namespace,
		FilePath:     path,
		Version:      release.VersionNumber,
		WebsiteURL:   release.WebsiteURL,
		Description:  release.Description,
		Dependencies: release.Dependencies,
	}
	if err := ms.r.UpsertMod(m); err != nil {
		return fmt.Errorf("mod files installed at %q, but metadata write failed; verify database state before retrying: %w", path, err)
	}
	return nil
}

func modMutationResult(cleanupErr, failure error) error {
	if failure != nil {
		return errors.Join(failure, cleanupErr)
	}
	if cleanupErr != nil {
		return &ModMutationCleanupError{Err: cleanupErr}
	}
	return nil
}
