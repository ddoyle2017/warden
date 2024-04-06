package command

import (
	"fmt"
	"warden/data/mod"

	"github.com/spf13/cobra"
)

func NewListCommand(repo mod.ModsRepo) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all currently installed mods and their versions",
		Run: func(cmd *cobra.Command, args []string) {
			prettyPrint(repo.ListMods())
		},
	}
	return cmd
}

func prettyPrint(mods []mod.Mod) {
	if len(mods) == 0 {
		fmt.Print("... no mods are installed...")
	}
	for _, m := range mods {
		fmt.Printf(" %s | %s | %s ", m.Name, m.Version, m.Description)
	}
}
