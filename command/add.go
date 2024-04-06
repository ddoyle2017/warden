package command

import (
	"warden/data/repo"
	"warden/domain/mod"

	"github.com/spf13/cobra"
)

func NewAddCommand(r repo.Mods) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Adds the specified mod",
		Long:  "Searches Thunderstone for the specified mod, downloads it, then adds it to your local mod collection",
		Run: func(cmd *cobra.Command, args []string) {
			// PLACEHOLDER
			m := mod.Mod{
				ID:           1,
				Name:         "Best Mod NA",
				FilePath:     "/your/file",
				Version:      "v0.1.0",
				WebsiteURL:   "something.github.com/probably",
				Description:  "If Shakespeare could code, this would be his MacBeth",
				Dependencies: []string{},
			}
			r.InsertMod(m)
		},
	}
	return cmd
}
