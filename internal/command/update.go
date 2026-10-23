package command

import (
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/service"

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
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if cmd.Flags().Changed(namespaceFlagLong) {
				err = ms.UpdateModByIdentity(namespace, modPkg)
			} else {
				err = ms.UpdateMod(modPkg)
			}
			if err != nil {
				return parseUpdateError(err)
			}
			fmt.Println("... mod successfully updated! ...")
			return nil
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ms.UpdateAllMods(); err != nil {
				return parseUpdateError(err)
			}
			return nil
		},
	}
	return cmd
}

func newUpdateBepInEx(fs service.Framework) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bepinex",
		Short: "Updates BepInEx.",
		Long:  "Updates the current BepInEx installation.",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := fs.UpdateBepInEx()
			if err != nil {
				return parseFrameworkError(err, "update")
			}
			fmt.Println("...successfully updated BepInEx!...")
			return nil
		},
	}
	return cmd
}

func parseUpdateError(err error) error {
	if err == service.ErrCancelled || err == service.ErrNoChanges {
		return nil
	}
	if _, modCompleted := err.(*service.ModMutationCleanupError); modCompleted {
		fmt.Printf("... warning: %v ...\n", err)
		return nil
	} else if errors.Is(err, service.ErrConfirmationInput) ||
		errors.Is(err, service.ErrInvalidReleaseVersion) {
		return runtimeFailure("... %w ...", err)
	} else if errors.Is(err, service.ErrModNameAmbiguous) || errors.Is(err, service.ErrDuplicateModRecords) {
		return runtimeFailure("... %w, update stopped ...", err)
	} else if errors.Is(err, service.ErrModNotInstalled) {
		return runtimeFailure("... %w, update stopped ...", err)
	} else if errors.Is(err, service.ErrUnableToUpdateMod) {
		return runtimeFailure("... %w ...", err)
	} else if errors.Is(err, service.ErrModNotFound) {
		return runtimeFailure("... could not find mod on Thunderstore, stopping update: %w ...", err)
	} else if errors.Is(err, service.ErrAddDependenciesFailed) {
		return runtimeFailure("... %w ...", err)
	} else if errors.Is(err, service.ErrMaxAttempts) {
		return runtimeFailure("... unable to confirm update, aborting: %w ...", err)
	}
	return runtimeFailure("... %w ...", err)
}
