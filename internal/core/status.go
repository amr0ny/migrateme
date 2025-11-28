package core

import (
	"context"
	"fmt"
)

func (m *Migrator) Status(ctx context.Context) ([]string, []string, error) {
	files, err := m.getMigrationFiles()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list migration files: %w", err)
	}

	applied, err := m.db.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	appliedSet := make(map[string]bool)
	for _, a := range applied {
		appliedSet[a] = true
	}

	var pending []string
	for _, file := range files {
		if !appliedSet[file] {
			pending = append(pending, file)
		}
	}

	return applied, pending, nil
}
