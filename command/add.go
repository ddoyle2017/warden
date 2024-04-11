package command

import (
	"errors"
	"fmt"
	"warden/api"
	"warden/api/thunderstore"
	"warden/data/file"
	"warden/data/repo"
	"warden/domain/mod"

	"github.com/spf13/cobra"
)

func NewAddCommand(r repo.Mods, ts thunderstore.Thunderstore, manager file.Manager) *cobra.Command {
	var namespace string
	var modPkg string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Adds the specified mod.",
		Long:  "Searches Thunderstone for the specified mod, downloads it, then adds it to your local mod collection.",
		Run: func(cmd *cobra.Command, args []string) {
			pkg, err := ts.GetPackage(namespace, modPkg)
			if err != nil {
				parseError(err)
				return
			}

			m := mod.Mod{
				Name:         pkg.Name,
				Namespace:    pkg.Namespace,
				FilePath:     "/your/file",
				Version:      pkg.Latest.VersionNumber,
				WebsiteURL:   pkg.Latest.WebsiteURL,
				Description:  pkg.Latest.Description,
				Dependencies: pkg.Latest.Dependencies,
			}
			err = r.InsertMod(m)
			if err != nil {
				fmt.Println("... failed to save mod ...")
			}
			err = manager.InstallMod(pkg.Latest.DownloadURL, pkg.Latest.FullName)
			if err != nil {
				fmt.Println("... failed to install mod ...")
				r.DeleteMod(m.Name, m.Namespace)
				fmt.Printf("%v+", err)
				return
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

func parseError(err error) {
	if errors.Is(err, thunderstore.ErrPackageNotFound) {
		fmt.Println("... unable to find mod package...")
	} else if errors.Is(err, thunderstore.ErrThunderstoreAPI) {
		fmt.Println("... Thunderstore.io is experiencing issues. Please try again later ...")
	} else if errors.Is(err, api.ErrByteIO) || errors.Is(err, api.ErrHTTPClient) || errors.Is(err, api.ErrJSONParse) {
		fmt.Println("... unexpected error processing Thunderstore.io request ...")
	}
}
