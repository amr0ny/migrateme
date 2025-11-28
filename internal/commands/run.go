package commands

import (
	"context"
	"fmt"
	"github.com/amr0ny/migrateme/internal/config"
	"github.com/amr0ny/migrateme/internal/core"
	"github.com/amr0ny/migrateme/internal/database"
	"github.com/spf13/cobra"
)

func NewRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Apply all pending migrations",
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

			applied, err := migrator.Run(ctx)
			if err != nil {
				return err
			}

			fmt.Printf("Applied %d migrations\n", len(applied))
			return nil
		},
	}

	return cmd
}
