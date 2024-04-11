package command

import "github.com/spf13/cobra"

func NewUpdateCommand() *cobra.Command {
	var modPkg string

	cmd := &cobra.Command{}

	cmd.Flags().StringVarP(&modPkg, modPackageFlagLong, modPackageFlagShort, "", modPackageFlagDesc)
	cmd.MarkFlagRequired(modPackageFlagLong)
	return cmd
}
