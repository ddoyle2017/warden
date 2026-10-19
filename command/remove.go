package command

import (
	"errors"
	"fmt"
	"warden/internal/service"

	"github.com/spf13/cobra"
)

func NewRemoveCommand(fs service.Framework, ms service.Mod) *cobra.Command {
	var namespace string
	var modPkg string

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Removes the specified mod.",
		Long:  "Deletes the mod from your mod folder and removes it from the local data storage.",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := ms.RemoveMod(namespace, modPkg)
			if err != nil {
				return parseRemoveError(err)
			}
			fmt.Println("... mod successfully removed! ...")
			return nil
		},
	}
	cmd.Flags().StringVarP(&namespace, namespaceFlagLong, namespaceFlagShort, "", namespaceFlagDesc)
	cmd.Flags().StringVarP(&modPkg, modPackageFlagLong, modPackageFlagShort, "", modPackageFlagDesc)

	cmd.MarkFlagRequired(namespaceFlagLong)
	cmd.MarkFlagRequired(modPackageFlagLong)
	cmd.MarkFlagsRequiredTogether(namespaceFlagLong, modPackageFlagLong)

	// Add sub-commands
	cmd.AddCommand(newRemoveAllCommand(ms))
	cmd.AddCommand(newRemoveBepInEx(fs))
	return cmd
}

func newRemoveAllCommand(ms service.Mod) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Removes all mods.",
		Long:  "Deletes all mods from your mod folder, and removes records of them from the local data storage.",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := ms.RemoveAllMods()
			if err != nil {
				return parseRemoveAllError(err)
			}
			fmt.Println("... all mods were removed successfully! ...")
			return nil
		},
	}
	return cmd
}

func newRemoveBepInEx(fs service.Framework) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bepinex",
		Short: "Removes BepInEx installation.",
		Long:  "Removes BepInEx and all mods installed under it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := fs.RemoveBepInEx()
			if err != nil {
				return parseFrameworkError(err, "remove")
			}
			fmt.Println("... BepInEx and mods were removed successfully! ...")
			return nil
		},
	}
	return cmd
}

func parseRemoveError(err error) error {
	if err == service.ErrCancelled || err == service.ErrNoChanges {
		return nil
	}
	if _, completed := err.(*service.ModRemovalCleanupError); completed {
		fmt.Printf("... warning: %v ...\n", err)
		return nil
	} else if errors.Is(err, service.ErrUnableToRemoveMod) || errors.Is(err, service.ErrConfirmationInput) {
		return runtimeFailure("... %w ...", err)
	} else if errors.Is(err, service.ErrMaxAttempts) {
		return runtimeFailure("... unable to confirm mod removal, aborting: %w ...", err)
	} else if errors.Is(err, service.ErrModNotInstalled) {
		return runtimeFailure("... %w ...", err)
	}
	return runtimeFailure("... unable to remove mod: %w ...", err)
}

func parseRemoveAllError(err error) error {
	if err == service.ErrCancelled || err == service.ErrNoChanges {
		return nil
	}
	if _, completed := err.(*service.ModRemovalCleanupError); completed {
		fmt.Printf("... warning: %v ...\n", err)
		return nil
	} else if errors.Is(err, service.ErrUnableToRemoveMod) || errors.Is(err, service.ErrConfirmationInput) {
		return runtimeFailure("... %w ...", err)
	} else if errors.Is(err, service.ErrMaxAttempts) {
		return runtimeFailure("... unable to confirm mod removal, aborting: %w ...", err)
	}
	return runtimeFailure("... unable to remove mods: %w ...", err)
}
