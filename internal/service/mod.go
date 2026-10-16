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
	// Check if the mod is already installed
	current, err := ms.r.GetModByIdentity(namespace, name)

	if err == nil && !current.Equals(&mod.Mod{}) {
		// Mod already installed
		return ErrModAlreadyInstalled
	}
	if err != nil && !errors.Is(err, repo.ErrModFetchNoResults) {
		// If repo fetch returns any error BESIDES no results,
		// return mod install failure
		return fmt.Errorf("%w: %w", ErrModInstallFailed, err)
	}

	// Find the requested mod online
	pkg, err := ms.ts.GetPackage(namespace, name)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrModNotFound, err)
	}

	// Install the mod and it's dependencies
	cleanupErr, err := ms.installMod(pkg.Latest)
	if err != nil {
		return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", ErrModInstallFailed, err))
	}

	if len(pkg.Latest.Dependencies) > 0 {
		fmt.Printf("... mod has %d dependencies, installing them ...\n", len(pkg.Latest.Dependencies))

		dependencyCleanup, err := ms.addDependencies(pkg.Latest.Dependencies)
		cleanupErr = errors.Join(cleanupErr, dependencyCleanup)
		if err != nil {
			return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", ErrAddDependenciesFailed, err))
		}
	}
	return modMutationResult(cleanupErr, nil)
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
		cleanupErr, err := ms.updateMod(current.FullName(), pkg.Latest)
		if err != nil {
			return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", ErrUnableToUpdateMod, err))
		}
		dependencyCleanup, err := ms.addDependencies(pkg.Latest.Dependencies)
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

	// For each one, check if there's an update and install it if there is
	var cleanupErr error
	for _, m := range mods {
		if identityCounts[[2]string{m.Namespace, m.Name}] > 1 {
			return modMutationResult(cleanupErr, fmt.Errorf("%w: %w for %s/%s; inspect installation records before retrying: %w",
				ErrUnableToUpdateMod, ErrDuplicateModRecords, m.Namespace, m.Name, repo.ErrModFetchMultipleResults))
		}
		pkg, err := ms.ts.GetPackage(m.Namespace, m.Name)
		if err != nil {
			return modMutationResult(cleanupErr, fmt.Errorf("%w: %s/%s: %w", ErrModNotFound, m.Namespace, m.Name, err))
		}

		newer, err := hasNewerRelease(m.Version, pkg.Latest.VersionNumber)
		if err != nil {
			return modMutationResult(cleanupErr, fmt.Errorf("%w: %s/%s: %w", ErrUnableToUpdateMod, m.Namespace, m.Name, err))
		}
		if newer {
			updateCleanup, err := ms.updateMod(m.FullName(), pkg.Latest)
			cleanupErr = errors.Join(cleanupErr, updateCleanup)
			if err != nil {
				return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", ErrUnableToUpdateMod, err))
			}
			dependencyCleanup, err := ms.addDependencies(pkg.Latest.Dependencies)
			cleanupErr = errors.Join(cleanupErr, dependencyCleanup)
			if err != nil {
				return modMutationResult(cleanupErr, fmt.Errorf("%w: %w", ErrAddDependenciesFailed, err))
			}
		} else {
			fmt.Printf("... latest version of %s %s already installed (%s) ...\n", m.Namespace, m.Name, m.Version)
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

func (ms *modService) addDependencies(dependencies []string) (cleanupErr, failure error) {
	for _, dep := range dependencies {
		details := strings.Split(dep, "-")
		namespace, name := details[0], details[1]

		// If dep is BepInEx, skip
		if name == framework.BepInEx {
			continue
		}

		pkg, err := ms.ts.GetPackage(namespace, name)
		if err != nil {
			return cleanupErr, fmt.Errorf("%s/%s: %w", namespace, name, err)
		}
		installCleanup, err := ms.installMod(pkg.Latest)
		cleanupErr = errors.Join(cleanupErr, installCleanup)
		if err != nil {
			return cleanupErr, err
		}
	}
	return cleanupErr, nil
}

func (ms *modService) updateMod(fullname string, latest thunderstore.Release) (cleanupErr, failure error) {
	if err := ms.fm.ValidateModInstall(); err != nil {
		return nil, err
	}

	// Delete the previous mod files
	err := ms.fm.RemoveMod(fullname)
	if err != nil {
		if _, completed := err.(*file.ModRemovalCleanupError); !completed {
			return nil, err
		}
		cleanupErr = fmt.Errorf("%s: %w", fullname, err)
	}
	// This path removed the prior release and owns cleanup of its obsolete record.
	path, err := ms.fm.InstallMod(latest.DownloadURL, latest.FullName)
	if err != nil {
		if _, completed := err.(*file.ModInstallCleanupError); !completed {
			recordErr := ms.r.DeleteMod(latest.Name, latest.Namespace)
			if recordErr != nil {
				recordErr = fmt.Errorf("obsolete record deletion failed; verify database state before retrying: %w", recordErr)
			}
			return cleanupErr, errors.Join(fmt.Errorf("%s: %w", latest.FullName, err), recordErr)
		}
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("%s: %w", latest.FullName, err))
	}
	return cleanupErr, ms.recordMod(latest, path)
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
