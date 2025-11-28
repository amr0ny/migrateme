package cli

import (
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Database migration tool",
	}

	cmd.AddCommand(NewGenerateCommand())
	cmd.AddCommand(NewRunCommand())
	cmd.AddCommand(NewStatusCommand())
	cmd.AddCommand(NewRollbackCommand())
	cmd.AddCommand(NewCreateCommand())
	cmd.AddCommand(NewDiscoverCommand())

	return cmd
}
