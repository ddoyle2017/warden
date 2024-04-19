package command

import (
	"errors"
	"fmt"
	"warden/service"

	"github.com/spf13/cobra"
)

func NewRemoveCommand(ms service.ModService) *cobra.Command {
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
	return cmd
}

func newRemoveAllCommand(ms service.ModService) *cobra.Command {
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

func parseRemoveError(err error) {
	if errors.Is(err, service.ErrUnableToRemoveMod) {
		fmt.Println("... unable to remove mod ...")
	} else if errors.Is(err, service.ErrMaxAttempts) {
		fmt.Println("... unable to confim mod removal, aborting ...")
	} else if errors.Is(err, service.ErrModNotInstalled) {
		fmt.Println("... mod not installed ...")
	}
}

func parseRemoveAllError(err error) {
	if errors.Is(err, service.ErrUnableToRemoveMod) {
		fmt.Println("... unable to remove mods ...")
	} else if errors.Is(err, service.ErrMaxAttempts) {
		fmt.Println("... unable to confim mod removal, aborting ...")
	}
}
