// Package images provides CLI commands for managing lab images.
package images

import (
	"github.com/spf13/cobra"
)

// Cmd is the images subcommand.
var Cmd = &cobra.Command{
	Use:   "images",
	Short: "Manage lab images",
	Long:  "Commands for syncing, validating, listing, pruning, and uploading lab images to e2 storage.",
}

func init() {
	Cmd.AddCommand(syncCmd)
	Cmd.AddCommand(validateCmd)
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(pruneCmd)
	Cmd.AddCommand(uploadCmd)
	Cmd.AddCommand(downloadCmd)
}
