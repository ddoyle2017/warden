package command

import (
	"errors"
	"fmt"
	"warden/internal/service"

	"github.com/spf13/cobra"
)

func NewStartCommand(server service.Server) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Starts the Valheim game server.",
		Long:  "Starts the Valheim game server using the given configurattion, either vanilla or modded.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			if server.IsValidGameType(args[0]) {
				return nil
			}
			return fmt.Errorf("invalid game type %q: %w", args[0], service.ErrInvalidGameType)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			output, err := server.Start(args[0])
			if err != nil {
				if output != "" {
					fmt.Println(output)
				}
				return parseStartError(err)
			}
			fmt.Println("... successfully started server! ...")
			fmt.Println(output)
			return nil
		},
	}
	return cmd
}

func parseStartError(err error) error {
	if errors.Is(err, service.ErrInvalidGameType) {
		return runtimeFailure("... invalid game type: %w ...", err)
	}
	return runtimeFailure("... Valheim server failed to start: %w ...", err)
}
