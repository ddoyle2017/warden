package command

import (
	"fmt"
	"warden/data/repo"
	"warden/domain/mod"

	"github.com/spf13/cobra"
)

func NewListCommand(r repo.Mods) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all currently installed mods and their versions",
		Run: func(cmd *cobra.Command, args []string) {
			prettyPrint(r.ListMods())
		},
	}
	return cmd
}

func prettyPrint(mods []mod.Mod) {
	if len(mods) == 0 {
		fmt.Print("... no mods are installed...")
	}
	for _, m := range mods {
		fmt.Printf(" %s | %s | %s \n", m.Name, m.Version, m.Description)
	}
}
