package command

import (
	"fmt"
	"strings"
	"warden/internal/domain/mod"
	"warden/internal/service"

	"github.com/spf13/cobra"
)

const (
	nameWidth        = 20
	versionWidth     = 10
	descriptionWidth = 40
)

func NewListCommand(ms service.Mod) *cobra.Command {
	var isVerbose bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all currently installed mods and their versions",
		Run: func(cmd *cobra.Command, args []string) {
			mods, err := ms.ListMods()
			if err != nil {
				fmt.Println("... Unable to retrieve list of mods")
			}
			prettyPrint(mods, isVerbose)
		},
	}
	cmd.Flags().BoolVarP(&isVerbose, "verbose", "v", false, "Enable a more detailed version of the mods list")
	return cmd
}

func prettyPrint(mods []mod.Mod, isVerbose bool) {
	if len(mods) == 0 {
		fmt.Print("... No mods are installed")
		return
	}

	if isVerbose {
		// Print header
		fmt.Printf("%-*s %-*s %-*s\n", nameWidth, "NAME", versionWidth, "VERSION", descriptionWidth, "DESCRIPTION")
		fmt.Printf("%-*s %-*s %-*s\n", nameWidth, strings.Repeat("-", nameWidth), versionWidth, strings.Repeat("-", versionWidth), descriptionWidth, strings.Repeat("-", descriptionWidth))

		for _, mod := range mods {
			wrappedDescription := wrapString(mod.Description, descriptionWidth)
			fmt.Printf("%-*s %-*s %-*s\n", nameWidth, mod.Name, versionWidth, mod.Version, descriptionWidth, wrappedDescription[0])

			for _, line := range wrappedDescription[1:] {
				fmt.Printf("%-*s %-*s %-*s\n", nameWidth, "", versionWidth, "", descriptionWidth, line)
			}
			fmt.Println()
		}
	} else {
		for _, m := range mods {
			fmt.Printf("%s by %s | %s\n", m.Name, m.Namespace, m.Version)
		}
		fmt.Println()
	}
}

func wrapString(s string, width int) []string {
	var wrapped []string

	for len(s) > width {
		spaceIndex := strings.LastIndex(s[:width], " ")
		if spaceIndex == -1 {
			spaceIndex = width
		}

		wrapped = append(wrapped, s[:spaceIndex])
		s = strings.TrimSpace(s[spaceIndex:])
	}
	wrapped = append(wrapped, s)
	return wrapped
}
