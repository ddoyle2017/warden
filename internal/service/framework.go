package service

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
)

var (
	ErrUnableToInstallFramework = errors.New("unable to install mod framework")
	ErrUnableToUpdateFramework  = errors.New("unable to update mod framework")
	ErrUnableToRemoveFramework  = errors.New("unable to remove mod framework")

	ErrFrameworkNotInstalled = errors.New("framework is not installed")
	ErrFrameworkNotFound     = errors.New("framework package lookup failed")
)

// FrameworkMutationCleanupError marks a completed framework request, including
// metadata. Only a direct return from InstallBepInEx establishes bootstrap readiness.
type FrameworkMutationCleanupError struct {
	Err error
}

func (e *FrameworkMutationCleanupError) Error() string {
	return fmt.Sprintf("framework request completed; cleanup failed: %v", e.Err)
}

func (e *FrameworkMutationCleanupError) Unwrap() error {
	return e.Err
}

// Encapsulates all the business logic for managing frameworks, namely BepInEx. It coordinates
// both the database and file management to make sure they're updated together. Frameworks have
// separate installation rules than normal mods.
type Framework interface {
	InstallBepInEx() error
	UpdateBepInEx() error
	RemoveBepInEx() error
}

type frameworkService struct {
	fr repo.Frameworks
	fm file.Manager
	ts thunderstore.Thunderstore
	in *bufio.Scanner
}

func NewFrameworkService(fr repo.Frameworks, fm file.Manager, ts thunderstore.Thunderstore, reader io.Reader) Framework {
	return &frameworkService{
		fr: fr,
		fm: fm,
		ts: ts,
		in: bufio.NewScanner(reader),
	}
}

func (fs *frameworkService) InstallBepInEx() error {
	_, err := fs.fr.GetFramework(framework.BepInEx)
	if err == nil {
		return ErrNoChanges
	}
	if !errors.Is(err, repo.ErrFrameworkFetchNoResults) {
		return fmt.Errorf("%w: %w", ErrUnableToInstallFramework, err)
	}

	fmt.Println("... BepInEx installation is missing ...")
	fmt.Printf("did you want to install BepInEx? %s\n", yesOrNo)

	accepted, err := confirm(fs.in, yes, no)
	if err != nil {
		return err
	}
	if !accepted {
		fmt.Println("... aborting ...")
		return ErrCancelled
	}
	// Install BepInEx
	pkg, err := fs.ts.GetPackage(framework.BepInExNamespace, framework.BepInEx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFrameworkNotFound, err)
	}

	var installErr error
	_, err = fs.fm.InstallBepInEx(pkg.Latest.DownloadURL, pkg.Latest.FullName)
	if err != nil {
		installErr = fmt.Errorf("%w: %w", ErrUnableToInstallFramework, err)
		if _, completed := err.(*file.FrameworkMutationCleanupError); !completed {
			return installErr
		}
	}

	f := framework.Framework{
		Name:        pkg.Latest.Name,
		Namespace:   pkg.Latest.Namespace,
		Version:     pkg.Latest.VersionNumber,
		WebsiteURL:  pkg.Latest.WebsiteURL,
		Description: pkg.Latest.Description,
	}
	err = fs.fr.InsertFramework(f)
	if err != nil {
		return errors.Join(installErr, fmt.Errorf("%w: framework files installed, but metadata write failed; verify database state before retrying: %w", ErrUnableToInstallFramework, err))
	}
	if installErr != nil {
		return &FrameworkMutationCleanupError{Err: installErr}
	}

	fmt.Println("... successfully installed BepInEx ...")
	return nil
}

func (fs *frameworkService) UpdateBepInEx() error {
	// Check if BepInEx is installed
	current, err := fs.fr.GetFramework(framework.BepInEx)
	if err != nil && errors.Is(err, repo.ErrFrameworkFetchNoResults) {
		return fmt.Errorf("%w: %w", ErrFrameworkNotInstalled, err)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnableToUpdateFramework, err)
	}
	// Check if current version is the latest
	pkg, err := fs.ts.GetPackage(framework.BepInExNamespace, framework.BepInEx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnableToUpdateFramework, err)
	}

	newer, err := hasNewerRelease(current.Version, pkg.Latest.VersionNumber)
	if err != nil {
		return fmt.Errorf("%w: %s/%s: %w", ErrUnableToUpdateFramework, current.Namespace, current.Name, err)
	}
	if newer {
		fmt.Printf("... a new version of BepInEx was found (%s) ...\n", pkg.Latest.VersionNumber)
		fmt.Printf("did you want to update BepInEx? %s\n", yesOrNo)

		accepted, err := confirm(fs.in, yes, no)
		if err != nil {
			return err
		}
		if !accepted {
			fmt.Println("... aborting ...")
			return ErrCancelled
		}
		var updateErr error
		if err := fs.fm.UpdateBepInEx(pkg.Latest.DownloadURL, pkg.Latest.FullName); err != nil {
			updateErr = fmt.Errorf("%w: %w", ErrUnableToUpdateFramework, err)
			if _, completed := err.(*file.FrameworkMutationCleanupError); !completed {
				return updateErr
			}
		}

		f := framework.Framework{
			ID:          current.ID,
			Name:        pkg.Latest.Name,
			Namespace:   pkg.Latest.Namespace,
			Version:     pkg.Latest.VersionNumber,
			WebsiteURL:  pkg.Latest.WebsiteURL,
			Description: pkg.Latest.Description,
		}
		err = fs.fr.UpdateFramework(f)
		if err != nil {
			return errors.Join(updateErr, fmt.Errorf("%w: framework files updated, but metadata write failed; verify database state before retrying: %w", ErrUnableToInstallFramework, err))
		}
		if updateErr != nil {
			return &FrameworkMutationCleanupError{Err: updateErr}
		}
		return nil
	} else {
		fmt.Println("... BepInEx is up-to-date! ...")
	}
	return ErrNoChanges
}

func (fs *frameworkService) RemoveBepInEx() error {
	fmt.Printf("are you sure you want to remove BepInEx? %s\n", yesOrNoLong)

	accepted, err := confirm(fs.in, yesLong, noLong)
	if err != nil {
		return err
	}
	if !accepted {
		fmt.Println("... aborting ...")
		return ErrCancelled
	}
	var removeErr error
	if err := fs.fm.RemoveBepInEx(); err != nil {
		removeErr = fmt.Errorf("%w: %w", ErrUnableToRemoveFramework, err)
		if _, completed := err.(*file.FrameworkMutationCleanupError); !completed {
			return removeErr
		}
	}
	if err := fs.fr.DeleteFramework(framework.BepInEx, fs.fm.BepInExPluginDirectory()); err != nil {
		return errors.Join(removeErr, fmt.Errorf("%w: framework files were removed, but metadata deletion failed; verify database state before retrying: %w", ErrUnableToRemoveFramework, err))
	}
	if removeErr != nil {
		return &FrameworkMutationCleanupError{Err: removeErr}
	}
	return nil
}
