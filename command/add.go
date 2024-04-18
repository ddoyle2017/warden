package command

import (
	"errors"
	"fmt"
	"warden/service"

	"github.com/spf13/cobra"
)

func NewAddCommand(ms service.ModService) *cobra.Command {
	var namespace string
	var modPkg string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Adds the specified mod.",
		Long:  "Searches Thunderstone for the specified mod, downloads it, then adds it to your local mod collection.",
		Run: func(cmd *cobra.Command, args []string) {
			err := ms.AddMod(namespace, modPkg)
			if err != nil {
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
	if errors.Is(err, service.ErrModAlreadyInstalled) {
		fmt.Println("... mod already installed ...")
	} else if errors.Is(err, service.ErrModInstallFailed) {
		fmt.Println("... unable to install mod ...")
	} else if errors.Is(err, service.ErrModNotFound) {
		fmt.Println("... unable to find mod on Thunderstore")
	} else if errors.Is(err, service.ErrAddDependenciesFailed) {
		fmt.Println("... unable to install mod's dependencies...")
	}
}
