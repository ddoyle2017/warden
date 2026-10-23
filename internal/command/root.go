package command

import (
	"fmt"

	"github.com/spf13/cobra"
)

func Execute(cmds ...*cobra.Command) error {
	root := &cobra.Command{
		Use:           "warden",
		Short:         "Warden is a CLI mod manager for Valheim",
		Long:          `A fast and friendly CLI mod manager for Valheim. Built with love and Go for handling mods on headless servers <3`,
		SilenceErrors: true,
		SilenceUsage:  true,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(logo)
			fmt.Println(runes)
			fmt.Println(startUpBlurb)
		},
	}
	root.AddCommand(cmds...)
	executed, err := root.ExecuteC()
	if err != nil {
		executed.PrintErrln("Error:", err)
		if _, runtime := err.(*runtimeError); !runtime {
			executed.PrintErrln(executed.UsageString())
		}
	}
	return err
}

// Runtime failures do not need usage, but later input failures still do. Keep
// that distinction in the result rather than mutating a command's SilenceUsage.
type runtimeError struct {
	cause error
}

func (e *runtimeError) Error() string {
	return e.cause.Error()
}

func (e *runtimeError) Unwrap() error {
	return e.cause
}

func runtimeFailure(format string, args ...any) error {
	return &runtimeError{cause: fmt.Errorf(format, args...)}
}
