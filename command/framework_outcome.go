package command

import (
	"errors"
	"fmt"
	"warden/internal/service"
)

func parseFrameworkError(err error, operation string) error {
	if err == service.ErrCancelled || err == service.ErrNoChanges {
		return nil
	}
	if _, completed := err.(*service.FrameworkMutationCleanupError); completed {
		fmt.Printf("... warning: %v ...\n", err)
		return nil
	} else if errors.Is(err, service.ErrFrameworkNotFound) {
		return runtimeFailure("... unable to find BepInEx: %w ...", err)
	} else if errors.Is(err, service.ErrFrameworkNotInstalled) {
		return runtimeFailure("... BepInEx is not installed: %w ...", err)
	}
	return runtimeFailure("... unable to %s BepInEx: %w ...", operation, err)
}
