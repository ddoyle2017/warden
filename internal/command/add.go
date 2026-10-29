package command

import (
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/service"

	"github.com/spf13/cobra"
)

func NewAddCommand(fs service.Framework, ms service.Mod) *cobra.Command {
	var namespace string
	var modPkg string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Adds the specified mod.",
		Long:  "Searches Thunderstone for the specified mod, downloads it, then adds it to your local mod collection.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := fs.InstallBepInEx(); err == service.ErrCancelled {
				return nil
			} else if err != nil && err != service.ErrNoChanges {
				if err := parseFrameworkError(err, "install"); err != nil {
					return err
				}
			}
			if err := ms.AddMod(namespace, modPkg); err != nil {
				return parseAddError(err)
			}
			fmt.Println("... successfully installed mod! ...")
			return nil
		},
	}
	cmd.Flags().StringVarP(&namespace, namespaceFlagLong, namespaceFlagShort, "", namespaceFlagDesc)
	cmd.Flags().StringVarP(&modPkg, modPackageFlagLong, modPackageFlagShort, "", modPackageFlagDesc)

	cmd.MarkFlagRequired(namespaceFlagLong)
	cmd.MarkFlagRequired(modPackageFlagLong)
	cmd.MarkFlagsRequiredTogether(namespaceFlagLong, modPackageFlagLong)
	return cmd
}

func parseAddError(err error) error {
	if err == service.ErrCancelled || err == service.ErrNoChanges {
		return nil
	}
	if _, modCompleted := err.(*service.ModMutationCleanupError); modCompleted {
		fmt.Printf("... warning: %v ...\n", err)
		return nil
	} else if err == service.ErrModAlreadyInstalled {
		fmt.Println("... mod already installed ...")
		return nil
	} else if errors.Is(err, service.ErrModNotFound) {
		return runtimeFailure("... unable to find mod on Thunderstore: %w ...", err)
	}
	return runtimeFailure("... %w ...", err)
}
