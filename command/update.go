package command

import (
	"errors"
	"fmt"
	"warden/service"

	"github.com/spf13/cobra"
)

func NewUpdateCommand(ms service.ModService) *cobra.Command {
	var modPkg string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Updates the targetted mod.",
		Long:  "Finds the latest version of the mod on Thunderstore and updates the currently installed version with the new one.",
		Run: func(cmd *cobra.Command, args []string) {
			err := ms.UpdateMod(modPkg)
			if err != nil {
				parseUpdateError(err)
			} else {
				fmt.Println("... mod successfully updated! ...")
			}
		},
	}

	cmd.Flags().StringVarP(&modPkg, modPackageFlagLong, modPackageFlagShort, "", modPackageFlagDesc)
	cmd.MarkFlagRequired(modPackageFlagLong)

	// Add sub-commands
	cmd.AddCommand(newUpdateAllCommand(ms))
	return cmd
}

func newUpdateAllCommand(ms service.ModService) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Updates all mods",
		Long:  "Installs the latest version of every mod that is currently installed",
		Run: func(cmd *cobra.Command, args []string) {
			err := ms.UpdateAllMods()
			if err != nil {
				parseUpdateError(err)
			} else {
				fmt.Println("... all mods successfully updated! ...")
			}
		},
	}
	return cmd
}

func parseUpdateError(err error) {
	if errors.Is(err, service.ErrModNotInstalled) {
		fmt.Println("... mod not installed, update stopped ...")
	} else if errors.Is(err, service.ErrUnableToUpdateMod) {
		fmt.Println("... unable to update mod ...")
	} else if errors.Is(err, service.ErrModNotFound) {
		fmt.Println("... could not find mod on Thunderstore, stopping update ...")
	} else if errors.Is(err, service.ErrAddDependenciesFailed) {
		fmt.Println("... unable to update mod's depedencies, stopping update ...")
	} else if errors.Is(err, service.ErrMaxAttempts) {
		fmt.Println("... unable to confim update, aborting ...")
	}
}
