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
		RunE: func(cmd *cobra.Command, args []string) error {
			mods, err := ms.ListMods()
			if err != nil {
				return runtimeFailure("... unable to retrieve list of mods: %w ...", err)
			}
			prettyPrint(mods)
			return nil
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
