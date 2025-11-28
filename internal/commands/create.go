package commands

import (
	"fmt"
	"github.com/amr0ny/migrateme/internal/config"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an empty migration file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			name := strings.ToLower(args[0])
			name = strings.ReplaceAll(name, " ", "_")

			ts := time.Now().UTC().Format("20060102150405")
			file := fmt.Sprintf("%s__%s.sql", ts, name)

			path := filepath.Join(cfg.GetMigrationsDir(), file)
			if err := os.WriteFile(path, []byte("-- +migrate Up\n\n-- +migrate Down\n"), 0644); err != nil {
				return err
			}

			fmt.Println("Created", path)
			return nil
		},
	}

	return cmd
}
