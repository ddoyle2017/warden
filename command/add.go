package command

import (
	"fmt"
	"warden/api/thunderstore"
	"warden/data/file"
	"warden/data/repo"

	"github.com/spf13/cobra"
)

func NewAddCommand(r repo.Mods, ts thunderstore.Thunderstore, fm file.Manager) *cobra.Command {
	var namespace string
	var modPkg string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Adds the specified mod.",
		Long:  "Searches Thunderstone for the specified mod, downloads it, then adds it to your local mod collection.",
		Run: func(cmd *cobra.Command, args []string) {
			pkg, err := ts.GetPackage(namespace, modPkg)
			if err != nil {
				parseThunderstoreAPIError(err)
				return
			}
			err = addMod(r, fm, pkg.Latest)
			if err != nil {
				fmt.Println("... failed to install mod ...")
			}

			dependencies := pkg.Latest.Dependencies
			if len(dependencies) > 0 {
				fmt.Printf("... mod has %d dependencies, installing them ...\n", len(dependencies))

				err = addDependencies(r, fm, ts, pkg.Latest.Dependencies)
				if err != nil {
					fmt.Println("... failed to install dependencies...")
				}
			}
			fmt.Println("... successfully installed mod! ...")
		},
	}
	cmd.Flags().StringVarP(&namespace, namespaceFlagLong, namespaceFlagShort, "", namespaceFlagDesc)
	cmd.Flags().StringVarP(&modPkg, modPackageFlagLong, modPackageFlagShort, "", modPackageFlagDesc)

	cmd.MarkFlagRequired(namespaceFlagLong)
	cmd.MarkFlagRequired(modPackageFlagLong)
	cmd.MarkFlagsRequiredTogether(namespaceFlagLong, modPackageFlagLong)
	return cmd
}
