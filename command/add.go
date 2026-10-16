package command

import (
	"errors"
	"fmt"
	"warden/internal/service"

	"github.com/spf13/cobra"
)

func NewAddCommand(fs service.Framework, ms service.Mod) *cobra.Command {
	var namespace string
	var modPkg string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Adds the specified mod.",
		Long:  "Searches Thunderstone for the specified mod, downloads it, then adds it to your local mod collection.",
		Run: func(cmd *cobra.Command, args []string) {
			if err := fs.InstallBepInEx(); err != nil && err != service.ErrNoChanges {
				parseFrameworkError(err, "install")
				if _, ready := err.(*service.FrameworkMutationCleanupError); !ready {
					return
				}
			}
			if err := ms.AddMod(namespace, modPkg); err != nil {
				parseAddError(err)
			} else {
				fmt.Println("... successfully installed mod! ...")
			}
		},
	}
	cmd.Flags().StringVarP(&namespace, namespaceFlagLong, namespaceFlagShort, "", namespaceFlagDesc)
	cmd.Flags().StringVarP(&modPkg, modPackageFlagLong, modPackageFlagShort, "", modPackageFlagDesc)

	cmd.MarkFlagRequired(namespaceFlagLong)
	cmd.MarkFlagRequired(modPackageFlagLong)
	cmd.MarkFlagsRequiredTogether(namespaceFlagLong, modPackageFlagLong)
	return cmd
}

func parseAddError(err error) {
	if err == service.ErrCancelled || err == service.ErrNoChanges {
		return
	}
	if _, modCompleted := err.(*service.ModMutationCleanupError); modCompleted {
		fmt.Printf("... warning: %v ...\n", err)
	} else if errors.Is(err, service.ErrConfirmationInput) {
		fmt.Printf("... %v ...\n", err)
	} else if err == service.ErrModAlreadyInstalled {
		fmt.Println("... mod already installed ...")
	} else if errors.Is(err, service.ErrModInstallFailed) {
		fmt.Printf("... %v ...\n", err)
	} else if errors.Is(err, service.ErrModNotFound) {
		fmt.Printf("... unable to find mod on Thunderstore: %v ...\n", err)
	} else if errors.Is(err, service.ErrAddDependenciesFailed) {
		fmt.Printf("... %v ...\n", err)
	} else {
		fmt.Printf("... %v ...\n", err)
	}
}
