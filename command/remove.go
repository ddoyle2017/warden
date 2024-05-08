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
			err := ms.RemoveMod(namespace, modPkg)
			if err != nil {
				parseRemoveError(err)
			} else {
				fmt.Println("... mod successfully removed! ...")
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
			err := ms.RemoveAllMods()
			if err != nil {
				parseRemoveAllError(err)
			} else {
				fmt.Println("... all mods were removed successfully! ...")
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
			err := fs.RemoveBepInEx()
			if err != nil {
				parseRemoveError(err)
			} else {
				fmt.Println("... BepInEx and mods were removed successfully! ...")
			}
		},
	}
	return cmd
}

func parseRemoveError(err error) {
	if errors.Is(err, service.ErrUnableToRemoveMod) {
		fmt.Println("... unable to remove mod ...")
	} else if errors.Is(err, service.ErrMaxAttempts) {
		fmt.Println("... unable to confim mod removal, aborting ...")
	} else if errors.Is(err, service.ErrModNotInstalled) {
		fmt.Println("... mod not installed ...")
	} else if errors.Is(err, service.ErrUnableToRemoveFramework) {
		fmt.Println("... unable to remove BepInEx ...")
	}
}

func parseRemoveAllError(err error) {
	if errors.Is(err, service.ErrUnableToRemoveMod) {
		fmt.Println("... unable to remove mods ...")
	} else if errors.Is(err, service.ErrMaxAttempts) {
		fmt.Println("... unable to confim mod removal, aborting ...")
	}
}
