package command

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rootCommand = &cobra.Command{
	Use:   "warden",
	Short: "Warden is a CLI mod manager for Valheim",
	Long:  `A fast and friendly CLI mod manager for Valheim. Built with love and Go for handling mods on headless servers <3`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(logo)
		fmt.Println(startUpBlurb)
	},
}

func Execute() {
	cobra.CheckErr(rootCommand.Execute())
}
