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
		Run: func(cmd *cobra.Command, args []string) {
			if err := ms.RemoveMod(namespace, modPkg); err != nil {
				parseRemoveError(err)
			}
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
		Run: func(cmd *cobra.Command, args []string) {
			if err := ms.RemoveAllMods(); err != nil {
				parseRemoveAllError(err)
			}
		},
	}
	return cmd
}

func newRemoveBepInEx(fs service.Framework) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bepinex",
		Short: "Removes BepInEx installation.",
		Long:  "Removes BepInEx and all mods installed under it.",
		Run: func(cmd *cobra.Command, args []string) {
			if err := fs.RemoveBepInEx(); err != nil {
				parseRemoveError(err)
			}
		},
	}
	return cmd
}

func parseRemoveError(err error) {
	if errors.Is(err, service.ErrUnableToRemoveMod) {
		fmt.Println("... Unable to remove mod")
	} else if errors.Is(err, service.ErrMaxAttempts) {
		fmt.Println("... Unable to confim mod removal, aborting")
	} else if errors.Is(err, service.ErrModNotInstalled) {
		fmt.Println("... Mod not installed")
	} else if errors.Is(err, service.ErrUnableToRemoveFramework) {
		fmt.Println("... Unable to remove BepInEx")
	} else if errors.Is(err, service.ErrFrameworkNotInstalled) {
		fmt.Println("... BepInEx not installed")
	}
}

func parseRemoveAllError(err error) {
	if errors.Is(err, service.ErrUnableToRemoveMod) {
		fmt.Println("... Unable to remove mods")
	} else if errors.Is(err, service.ErrMaxAttempts) {
		fmt.Println("... Unable to confim mod removal, aborting")
	}
}
