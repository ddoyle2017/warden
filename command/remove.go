package command

import (
	"bufio"
	"fmt"
	"os"
	"warden/data/file"
	"warden/data/repo"

	"github.com/spf13/cobra"
)

func NewRemoveCommand(r repo.Mods, fm file.Manager) *cobra.Command {
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
					fmt.Println("... mod successfully removed! ...")
					return
				} else if scanner.Text() == "n" {
					fmt.Println("... aborting ...")
					return
				}
			}
		},
	}
	cmd.Flags().StringVarP(&namespace, namespaceFlagLong, namespaceFlagShort, "", namespaceFlagDesc)
	cmd.Flags().StringVarP(&modPkg, modPackageFlagLong, modPackageFlagShort, "", modPackageFlagDesc)

	cmd.MarkFlagRequired(namespaceFlagLong)
	cmd.MarkFlagRequired(modPackageFlagLong)
	cmd.MarkFlagsRequiredTogether(namespaceFlagLong, modPackageFlagLong)

	// Add sub-commands
	cmd.AddCommand(newRemoveAllCommand(r, fm))
	return cmd
}

func newRemoveAllCommand(r repo.Mods, fm file.Manager) *cobra.Command {
	scanner := bufio.NewScanner(os.Stdin)

	cmd := &cobra.Command{
		Use:   "all",
		Short: "Removes all mods.",
		Long:  "Deletes all mods from your mod folder, and removes records of them from the local data storage.",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("are you sure you want to remove ALL mods? [YES I AM/no]")

			for scanner.Scan() {
				if scanner.Text() == "YES I AM" {
					errRepo := r.DeleteAllMods()
					errFile := fm.RemoveAllMods()

					if errRepo != nil || errFile != nil {
						fmt.Println("... unable to remove mods ...")
					}
					fmt.Println("... all mods were removed successfully! ...")
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
