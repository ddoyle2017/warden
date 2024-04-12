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
	scanner := bufio.NewScanner(os.Stdin)

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
				fmt.Printf("... found a new version (%s) of %s %s ...\n", pkg.Latest.VersionNumber, current.Namespace, current.Name)
				fmt.Println("did you want to update this mod? [Y/n]")

				for scanner.Scan() {
					if scanner.Text() == "Y" {
						updateMod(fm, current, pkg.Latest)
					} else if scanner.Text() == "n" {
						fmt.Println("... aborting ...")
						return
					}
				}
			} else {
				fmt.Printf("... latest version of %s %s already installed (%s) ...\n", current.Namespace, current.Name, current.Version)
			}
		},
	}

	cmd.Flags().StringVarP(&modPkg, modPackageFlagLong, modPackageFlagShort, "", modPackageFlagDesc)
	cmd.MarkFlagRequired(modPackageFlagLong)

	// Add sub-commands
	cmd.AddCommand(newUpdateAllCommand(r, ts, fm))
	return cmd
}

func newUpdateAllCommand(r repo.Mods, ts thunderstore.Thunderstore, fm file.Manager) *cobra.Command {
	scanner := bufio.NewScanner(os.Stdin)

	cmd := &cobra.Command{
		Use:   "all",
		Short: "Updates all mods",
		Long:  "Installs the latest version of every mod that is currently installed",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("are you sure you wanted to update ALL mods? [Y/n]")

			for scanner.Scan() {
				if scanner.Text() == "Y" {
					// Get all installed mods
					mods, err := r.ListMods()
					if err != nil {
						parseRepoError(err)
						return
					}
					// For each one, check if there's an update and install it if there is
					for _, m := range mods {
						pkg, err := ts.GetPackage(m.Namespace, m.Name)
						if err != nil {
							parseThunderstoreAPIError(err)
							return
						}

						if m.Version < pkg.Latest.VersionNumber {
							updateMod(fm, m, pkg.Latest)
						} else {
							fmt.Printf("... latest version of %s %s already installed (%s) ...\n", m.Namespace, m.Name, m.Version)
						}
					}
					fmt.Println("... all mods successfully updated! ...")
					return
				} else if scanner.Text() == "no" {
					fmt.Println("... aborting ...")
					return
				}
			}
		},
	}
	return cmd
}

func updateMod(fm file.Manager, current mod.Mod, latest thunderstore.Release) {
	err := fm.RemoveMod(current.FullName())
	if err != nil {
		fmt.Println("... unable to remove current version ...")
		return
	}

	_, err = fm.InstallMod(latest.DownloadURL, latest.FullName)
	if err != nil {
		fmt.Println("... unable to install new version...")
	}
	// Update matching db record with new data
	fmt.Println("... mod successfully updated! ...")
}
