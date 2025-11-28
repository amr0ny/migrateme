package commands

import (
	"context"
	"fmt"
	"github.com/amr0ny/migrateme/internal/core"
	"github.com/amr0ny/migrateme/internal/database"
	"github.com/amr0ny/migrateme/pkg/config"
	"github.com/spf13/cobra"
)

func NewGenerateCommand() *cobra.Command {
	var migrationName string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "generate [migration-name]",
		Short: "Generate migration files based on schema diff",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				migrationName = args[0]
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

			result, err := migrator.Generate(ctx, core.GenerateOptions{
				MigrationName: migrationName,
				DryRun:        dryRun,
			})
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Println("DRY RUN - No files were created")
				fmt.Printf("Detected changes:\n")
				for _, change := range result.Changes {
					fmt.Printf("  - %s: %s (%s)\n", change.TableName, change.Type, change.Details)
				}
				return nil
			}

			if len(result.CreatedFiles) == 0 {
				fmt.Println("No changes detected - no migration files generated")
			} else {
				fmt.Printf("Generated migration files:\n")
				for _, file := range result.CreatedFiles {
					fmt.Printf("  - %s\n", file)
				}
				fmt.Printf("Total changes: %d tables modified\n", len(result.Changes))
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be generated without creating files")

	return cmd
}
