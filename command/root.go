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
		fmt.Println(runes)
		fmt.Println(startUpBlurb)
	},
}

func Execute(cmds ...*cobra.Command) {
	rootCommand.AddCommand(cmds...)
	cobra.CheckErr(rootCommand.Execute())
}
