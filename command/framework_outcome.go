package command

import (
	"errors"
	"fmt"
	"warden/internal/service"
)

func parseFrameworkError(err error, operation string) {
	if err == service.ErrCancelled || err == service.ErrNoChanges {
		return
	}
	if _, completed := err.(*service.FrameworkMutationCleanupError); completed {
		fmt.Printf("... warning: %v ...\n", err)
	} else if errors.Is(err, service.ErrFrameworkNotFound) {
		fmt.Printf("... unable to find BepInEx: %v ...\n", err)
	} else if errors.Is(err, service.ErrFrameworkNotInstalled) {
		fmt.Printf("... BepInEx is not installed: %v ...\n", err)
	} else {
		fmt.Printf("... unable to %s BepInEx: %v ...\n", operation, err)
	}
}
