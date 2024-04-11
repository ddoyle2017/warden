package command

import (
	"bufio"
	"fmt"
	"os"
	"warden/api/thunderstore"
	"warden/data/file"
	"warden/data/repo"
	"warden/domain/mod"

	"github.com/spf13/cobra"
)

func NewUpdateCommand(r repo.Mods, ts thunderstore.Thunderstore, fm file.Manager) *cobra.Command {
	var modPkg string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Updates the targetted mod.",
		Long:  "Finds the latest version of the mod on Thunderstore and updates the currently installed version with the new one.",
		Run: func(cmd *cobra.Command, args []string) {
			current, err := r.GetMod(modPkg)
			if err != nil {
				parseRepoError(err)
				return
			}

			pkg, err := ts.GetPackage(current.Namespace, current.Name)
			if err != nil {
				parseThunderstoreAPIError(err)
				return
			}

			if current.Version < pkg.Latest.VersionNumber {
				updateMod(fm, current, pkg.Latest)
			} else {
				fmt.Printf("... latest version of %s %s already installed (%s) ...\n", current.Namespace, current.Name, current.Version)
			}
		},
	}

	cmd.Flags().StringVarP(&modPkg, modPackageFlagLong, modPackageFlagShort, "", modPackageFlagDesc)
	cmd.MarkFlagRequired(modPackageFlagLong)
	return cmd
}

func updateMod(fm file.Manager, current mod.Mod, latest thunderstore.Release) {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Printf("... found a new version (%s) of %s %s ...\n", latest.VersionNumber, current.Namespace, current.Name)
	fmt.Println("did you want to update this mod? [Y/n]")

	for scanner.Scan() {
		if scanner.Text() == "Y" {
			fullname := current.Namespace + "-" + current.Name + "-" + current.Version
			err := fm.RemoveMod(fullname)
			if err != nil {
				fmt.Println("... unable to remove current version ...")
				return
			}
			err = fm.InstallMod(latest.DownloadURL, latest.FullName)
			if err != nil {
				fmt.Println("... unable to install new version...")
			}
			fmt.Println("... mod successfully updated! ...")
		} else if scanner.Text() == "n" {
			fmt.Println("... aborting ...")
			return
		}
	}
}
