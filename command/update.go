package command

import (
	"errors"
	"fmt"
	"warden/internal/service"

	"github.com/spf13/cobra"
)

func NewUpdateCommand(fs service.Framework, ms service.Mod) *cobra.Command {
	var namespace string
	var modPkg string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Updates the targetted mod.",
		Long:  "Updates an installed mod to the latest version on Thunderstore. Specify --namespace when multiple installed mods share the same name; --mod alone is accepted only for an unambiguous name.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			var err error
			if cmd.Flags().Changed(namespaceFlagLong) {
				err = ms.UpdateModByIdentity(namespace, modPkg)
			} else {
				err = ms.UpdateMod(modPkg)
			}
			if err != nil {
				parseUpdateError(err)
			} else {
				fmt.Println("... mod successfully updated! ...")
			}
		},
	}

	cmd.Flags().StringVarP(&namespace, namespaceFlagLong, namespaceFlagShort, "", "The namespace, AKA author, of the mod package (required if the name is ambiguous).")
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
			err := fs.UpdateBepInEx()
			if err != nil {
				parseFrameworkError(err, "update")
			} else {
				fmt.Println("...successfully updated BepInEx!...")
			}
		},
	}
	return cmd
}

func parseUpdateError(err error) {
	if err == service.ErrCancelled || err == service.ErrNoChanges {
		return
	}
	if _, modCompleted := err.(*service.ModMutationCleanupError); modCompleted {
		fmt.Printf("... warning: %v ...\n", err)
	} else if errors.Is(err, service.ErrConfirmationInput) ||
		errors.Is(err, service.ErrInvalidReleaseVersion) {
		fmt.Printf("... %v ...\n", err)
	} else if errors.Is(err, service.ErrModNameAmbiguous) || errors.Is(err, service.ErrDuplicateModRecords) {
		fmt.Printf("... %v, update stopped ...\n", err)
	} else if errors.Is(err, service.ErrModNotInstalled) {
		fmt.Println("... mod not installed, update stopped ...")
	} else if errors.Is(err, service.ErrUnableToUpdateMod) {
		fmt.Printf("... %v ...\n", err)
	} else if errors.Is(err, service.ErrModNotFound) {
		fmt.Printf("... could not find mod on Thunderstore, stopping update: %v ...\n", err)
	} else if errors.Is(err, service.ErrAddDependenciesFailed) {
		fmt.Printf("... %v ...\n", err)
	} else if errors.Is(err, service.ErrMaxAttempts) {
		fmt.Println("... unable to confim update, aborting ...")
	} else {
		fmt.Printf("... %v ...\n", err)
	}
}
