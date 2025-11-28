// internal/commands/discover.go
package cli

import (
	"fmt"
	"github.com/amr0ny/migrateme/pkg/config"
	"github.com/amr0ny/migrateme/pkg/generator"
	"github.com/spf13/cobra"
)

func NewDiscoverCommand() *cobra.Command {
	var output string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover migratable entities and generate registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			paths, err := config.ResolveEntityPaths(cfg.EntityPaths)
			if err != nil {
				return fmt.Errorf("failed to resolve entity paths: %w", err)
			}

			fmt.Println("Scanning source files:")
			for _, p := range paths {
				fmt.Println("  -", p)
			}

			// Обнаруживаем сущности
			entities, err := generator.DiscoverEntitiesForGeneration(paths)
			if err != nil {
				return fmt.Errorf("failed to discover entities: %w", err)
			}

			if len(entities) == 0 {
				fmt.Println("No migratable entities found.")
				return nil
			}

			fmt.Println("\nDiscovered entities:")
			for _, entity := range entities {
				fmt.Printf("  - %s.%s -> %s\n", entity.Package, entity.StructName, entity.TableName)
			}

			// Определяем путь для выходного файла
			if output == "" {
				output = "internal/migrator/registry.gen.go"
			}

			if dryRun {
				fmt.Printf("\nDRY RUN: Would generate registry at %s with %d entities\n",
					output, len(entities))
				return nil
			}

			// Генерируем файл регистрации
			if err := generator.GenerateRegistry(output, entities); err != nil {
				return fmt.Errorf("failed to generate registry: %w", err)
			}

			fmt.Printf("\n✅ Successfully generated registry at %s with %d entities\n",
				output, len(entities))
			fmt.Println("Run 'go generate' or 'go build' to apply the changes.")

			return nil
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path for generated registry")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be generated without creating files")

	return cmd
}
