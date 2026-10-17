package command

import (
	"fmt"
	"warden/internal/config"

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
	cmd.AddCommand(newConfigGetCommand())
	cmd.AddCommand(newConfigSetCommand())
	return cmd
}

func newConfigGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Print the configuration value.",
		Long:  "Print the value of the given configuration key.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			value := viper.Get(args[0])

			if value == nil {
				return runtimeFailure("configuration key does not exist: %q", args[0])
			}
			fmt.Println(value)
			return nil
		},
	}
	return cmd
}

func newConfigSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Updates the config value.",
		Long:  "Updates the configuration value for the given key. Changes are saved to .warden.yaml.",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]

			if !isValidConfigKey(key) {
				return runtimeFailure("'%s' is not a valid config setting", key)
			}
			// Save updated key in memory
			viper.Set(key, value)

			// Write change to file
			err := viper.WriteConfig()
			if err != nil {
				return runtimeFailure("unable to save configuration: %w", err)
			}
			return nil
		},
	}
	return cmd
}

func isValidConfigKey(key string) bool {
	switch key {
	case "valheim-directory":
		return true
	default:
		return false
	}
}
