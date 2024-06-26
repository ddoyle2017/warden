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
			return service.ErrServerStartFailed
		},
		Run: func(cmd *cobra.Command, args []string) {
			output, err := server.Start(args[0])
			if err != nil {
				parseStartError(err)
				fmt.Println(output)
			} else {
				fmt.Println("... successfully started server! ...")
				fmt.Println(output)
			}
		},
	}
	return cmd
}

func parseStartError(err error) {
	if !errors.Is(err, service.ErrInvalidGameType) {
		fmt.Println("... invalid game type ...")
	} else if !errors.Is(err, service.ErrServerStartFailed) {
		fmt.Println("... Valheim server failed to start ...")
	}
}
