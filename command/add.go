package command

import (
	"fmt"
	"warden/api/thunderstore"
	"warden/data/repo"
	"warden/domain/mod"

	"github.com/spf13/cobra"
)

const (
	namespaceFlag  = "namespace"
	modPackageFlag = "mod"
)

func NewAddCommand(r repo.Mods, ts thunderstore.Thunderstore) *cobra.Command {
	var namespace string
	var modPkg string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Adds the specified mod",
		Long:  "Searches Thunderstone for the specified mod, downloads it, then adds it to your local mod collection",
		Run: func(cmd *cobra.Command, args []string) {
			pkg, err := ts.GetPackage(namespace, modPkg)
			if err != nil {
				fmt.Println("... something broke ...")
				return
			}

			m := mod.Mod{
				Name:         pkg.Name,
				FilePath:     "/your/file",
				Version:      pkg.Latest.VersionNumber,
				WebsiteURL:   pkg.Latest.WebsiteURL,
				Description:  pkg.Latest.Description,
				Dependencies: pkg.Latest.Dependencies,
			}
			r.InsertMod(m)
		},
	}
	cmd.Flags().StringVarP(&namespace, namespaceFlag, "n", "", "The namespace, AKA author, of the mod package (required).")
	cmd.Flags().StringVarP(&modPkg, modPackageFlag, "m", "", "The name of the mod, AKA package, to add (required).")

	cmd.MarkFlagRequired(namespaceFlag)
	cmd.MarkFlagRequired(modPackageFlag)
	cmd.MarkFlagsRequiredTogether(namespaceFlag, modPackageFlag)
	return cmd
}
