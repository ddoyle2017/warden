package command

import (
	"errors"
	"fmt"
	"warden/internal/service"

	"github.com/spf13/cobra"
)

func NewUpdateCommand(fs service.Framework, ms service.Mod) *cobra.Command {
	var modPkg string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Updates the targetted mod.",
		Long:  "Finds the latest version of the mod on Thunderstore and updates the currently installed version with the new one.",
		Run: func(cmd *cobra.Command, args []string) {
			if err := ms.UpdateMod(modPkg); err != nil {
				parseUpdateError(err)
			}
		},
	}

	cmd.Flags().StringVarP(&modPkg, modPackageFlagLong, modPackageFlagShort, "", modPackageFlagDesc)
	cmd.MarkFlagRequired(modPackageFlagLong)

	// Add sub-commands
	cmd.AddCommand(newUpdateAllCommand(ms))
	cmd.AddCommand(newUpdateBepInEx(fs))
	return cmd
}

func newUpdateAllCommand(ms service.Mod) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Updates all mods",
		Long:  "Installs the latest version of every mod that is currently installed",
		Run: func(cmd *cobra.Command, args []string) {
			if err := ms.UpdateAllMods(); err != nil {
				parseUpdateError(err)
			}
		},
	}
	return cmd
}

func newUpdateBepInEx(fs service.Framework) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bepinex",
		Short: "Updates BepInEx.",
		Long:  "Updates the current BepInEx installation.",
		Run: func(cmd *cobra.Command, args []string) {
			if err := fs.UpdateBepInEx(); err != nil {
				parseUpdateError(err)
			}
		},
	}
	return cmd
}

func parseUpdateError(err error) {
	if errors.Is(err, service.ErrModNotInstalled) {
		fmt.Println("...Mod not installed, update stopped")
	} else if errors.Is(err, service.ErrUnableToUpdateMod) {
		fmt.Println("...Unable to update mod")
	} else if errors.Is(err, service.ErrModNotFound) {
		fmt.Println("... could not find mod on Thunderstore, stopping update")
	} else if errors.Is(err, service.ErrAddDependenciesFailed) {
		fmt.Println("...Unable to update mod's depedencies, stopping update")
	} else if errors.Is(err, service.ErrMaxAttempts) {
		fmt.Println("...Unable to confim update, aborting")
	} else if errors.Is(err, service.ErrFrameworkNotInstalled) {
		fmt.Println("...BepInEx is not installed")
	} else if errors.Is(err, service.ErrUnableToUpdateFramework) {
		fmt.Println("...Unable to update BepInEx")
	}
}
