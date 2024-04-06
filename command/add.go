package command

import (
	"fmt"
	"warden/api/thunderstore"
	"warden/data/repo"
	"warden/domain/mod"

	"github.com/spf13/cobra"
)

func NewAddCommand(r repo.Mods, ts thunderstore.Thunderstore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Adds the specified mod",
		Long:  "Searches Thunderstone for the specified mod, downloads it, then adds it to your local mod collection",
		Run: func(cmd *cobra.Command, args []string) {
			pkg, err := ts.GetPackage("Azumatt", "Where_You_At")
			if err != nil {
				fmt.Println("... something broke ...")
				return
			}

			// PLACEHOLDER
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
	return cmd
}
