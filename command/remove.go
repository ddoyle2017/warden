package command

import (
	"bufio"
	"fmt"
	"os"
	"warden/data/repo"

	"github.com/spf13/cobra"
)

func NewRemoveCommand(r repo.Mods) *cobra.Command {
	var namespace string
	var modPkg string
	scanner := bufio.NewScanner(os.Stdin)

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Removes the specified mod.",
		Long:  "Deletes the mod from your mod folder and removes it from the local data storage.",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("are you sure you want to remove this mod? [Y/n]")

			for scanner.Scan() {
				if scanner.Text() == "Y" {
					err := r.DeleteMod(modPkg, namespace)
					if err != nil {
						fmt.Println("... unable to remove mod ...")
					}
					fmt.Println("... mod successfully removed...")
					return
				} else if scanner.Text() == "n" {
					fmt.Println("... aborting ...")
					return
				}
			}
		},
	}
	cmd.Flags().StringVarP(&namespace, namespaceFlag, "n", "", "The namespace, AKA author, of the mod package (required).")
	cmd.Flags().StringVarP(&modPkg, modPackageFlag, "m", "", "The name of the mod, AKA package, to add (required).")

	cmd.MarkFlagRequired(namespaceFlag)
	cmd.MarkFlagRequired(modPackageFlag)
	cmd.MarkFlagsRequiredTogether(namespaceFlag, modPackageFlag)

	return cmd
}
