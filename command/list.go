package command

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCommand.AddCommand(listCommand)
}

var listCommand = &cobra.Command{
	Use:   "list",
	Short: "List all currently installed mods and their versions",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("MY MODS")
	},
}
