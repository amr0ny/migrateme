package commands

import (
	"context"
	"fmt"
	"github.com/amr0ny/migrateme/internal/core"
	"github.com/amr0ny/migrateme/internal/database"
	"github.com/amr0ny/migrateme/pkg/config"
	"github.com/spf13/cobra"
)

func NewStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show applied and pending migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			ctx := context.Background()
			db, err := database.NewDB(ctx, cfg.GetDSN())
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer db.Close()

			migrator := core.NewMigrator(cfg, db)

			applied, pending, err := migrator.Status(ctx)
			if err != nil {
				return err
			}

			fmt.Println("Applied:")
			for _, m := range applied {
				fmt.Println("  ✔", m)
			}

			fmt.Println("\nPending:")
			for _, f := range pending {
				fmt.Println("  ✘", f)
			}

			return nil
		},
	}

	return cmd
}
