package command

import (
	"fmt"
	"warden/config"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewConfigCommand(cfg config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Prints the current config of the app.",
		Long:  "Prints out all current configuration values for Warden. These are stored in .warden.yaml in your $HOME directory (by default)",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf(configTitle + "\n")
			fmt.Printf("\nUsing configuration file at: %s\n\n", viper.ConfigFileUsed())

			for _, key := range viper.AllKeys() {
				value := viper.Get(key)
				fmt.Printf("%s : %s\n", key, value)
			}
			fmt.Println()
		},
	}
	return cmd
}
