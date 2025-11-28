package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (m *Migrator) Rollback(ctx context.Context, n int) ([]string, error) {
	applied, err := m.db.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	if len(applied) == 0 {
		return []string{}, nil
	}

	// Определяем сколько миграций можем откатить
	if n > len(applied) {
		n = len(applied)
	}

	// Берем последние N миграций для отката
	toRollback := applied[len(applied)-n:]
	var rolledBack []string

	// Откатываем в обратном порядке
	for i := len(toRollback) - 1; i >= 0; i-- {
		base := toRollback[i]
		downFile := base + ".down.sql"
		downPath := filepath.Join(m.config.GetMigrationsDir(), downFile)

		// Проверяем существование down-файла
		if _, err := os.Stat(downPath); os.IsNotExist(err) {
			return rolledBack, fmt.Errorf("down file not found for migration: %s", base)
		}

		// Читаем down-миграцию
		content, err := os.ReadFile(downPath)
		if err != nil {
			return rolledBack, fmt.Errorf("read down file %s: %w", downFile, err)
		}

		downSQL := string(content)
		if strings.TrimSpace(downSQL) == "" {
			return rolledBack, fmt.Errorf("migration %s has empty down file", base)
		}

		// Выполняем откат
		if _, err := m.db.Pool.Exec(ctx, downSQL); err != nil {
			return rolledBack, fmt.Errorf("rollback %s: %w", base, err)
		}

		// Удаляем запись о примененной миграции
		if err := m.db.RemoveMigration(ctx, base); err != nil {
			return rolledBack, fmt.Errorf("remove migration %s: %w", base, err)
		}

		rolledBack = append(rolledBack, base)
	}

	return rolledBack, nil
}
