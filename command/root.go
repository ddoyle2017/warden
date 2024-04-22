package command

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

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

func init() {
	cobra.OnInitialize(initConfig)

	rootCommand.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.warden.yaml)")
	viper.BindPFlag("config", rootCommand.Flags().Lookup("config"))
}

func initConfig() {
	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func Execute(cmds ...*cobra.Command) {
	rootCommand.AddCommand(cmds...)
	cobra.CheckErr(rootCommand.Execute())
}
