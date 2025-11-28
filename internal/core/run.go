package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (m *Migrator) Run(ctx context.Context) ([]string, error) {
	if err := m.db.EnsureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure migrations table: %w", err)
	}

	files, err := m.getMigrationFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to list migration files: %w", err)
	}

	upFiles := filterUpFiles(files)
	migrationBases := extractMigrationBases(upFiles)

	applied, err := m.db.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	appliedSet := make(map[string]struct{})
	for _, a := range applied {
		appliedSet[a] = struct{}{}
	}

	var appliedNow []string

	for _, base := range migrationBases {
		if _, ok := appliedSet[base]; ok {
			continue
		}

		upFile := base + ".up.sql"
		upPath := filepath.Join(m.config.GetMigrationsDir(), upFile)

		content, err := os.ReadFile(upPath)
		if err != nil {
			return appliedNow, fmt.Errorf("read up file %s: %w", upFile, err)
		}

		upSQL := string(content)
		if strings.TrimSpace(upSQL) == "" {
			continue
		}

		if _, err := m.db.Pool.Exec(ctx, upSQL); err != nil {
			return appliedNow, fmt.Errorf("apply %s: %w", base, err)
		}

		if err := m.db.RecordMigration(ctx, base); err != nil {
			return appliedNow, fmt.Errorf("record migration %s: %w", base, err)
		}

		appliedNow = append(appliedNow, base)
	}

	return appliedNow, nil
}
