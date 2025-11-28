package cli

import (
	"context"
	"fmt"
	"github.com/amr0ny/migrateme/internal/core"
	"github.com/amr0ny/migrateme/internal/database"
	"github.com/amr0ny/migrateme/pkg/config"
	"github.com/spf13/cobra"
	"strconv"
)

func NewRollbackCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback <n>",
		Short: "Rollback last N applied migrations",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid number: %w", err)
			}
			if n <= 0 {
				return fmt.Errorf("N must be >= 1")
			}

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

			rolledBack, err := migrator.Rollback(ctx, n)
			if err != nil {
				return err
			}

			if len(rolledBack) == 0 {
				fmt.Println("No migrations to rollback")
			} else {
				fmt.Printf("Rolled back %d migrations: %v\n", len(rolledBack), rolledBack)
			}
			return nil
		},
	}

	return cmd
}
