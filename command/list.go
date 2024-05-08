package command

import (
	"fmt"
	"warden/internal/domain/mod"
	"warden/internal/service"

	"github.com/spf13/cobra"
)

func NewListCommand(ms service.Mod) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all currently installed mods and their versions",
		Run: func(cmd *cobra.Command, args []string) {
			mods, err := ms.ListMods()
			if err != nil {
				fmt.Println("... unable to retrieve list of mods ...")
			}
			prettyPrint(mods)
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
