package config

import (
	"context"
	"fmt"
	"github.com/amr0ny/migrateme/internal/infrastructure/postgres"
	"github.com/amr0ny/migrateme/pkg/migrate"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

type DatabaseConfig struct {
	DSN            string `yaml:"dsn" env:"DATABASE_DSN"`
	MaxConnections int    `yaml:"max_connections" env:"DATABASE_MAX_CONNS"`
	MinConnections int    `yaml:"min_connections" env:"DATABASE_MIN_CONNS"`
}

type MigrationsConfig struct {
	Dir       string `yaml:"dir" env:"MIGRATIONS_DIR"`
	TableName string `yaml:"table_name" env:"MIGRATIONS_TABLE"`
}

type LoggingConfig struct {
	Level  string `yaml:"level" env:"LOG_LEVEL"`
	Format string `yaml:"format" env:"LOG_FORMAT"`
}

type Config struct {
	Database   DatabaseConfig   `yaml:"database"`
	Migrations MigrationsConfig `yaml:"migrations"`
	Logging    LoggingConfig    `yaml:"logging"`

	AutoRegister bool     `yaml:"auto_register"`
	EntityPaths  []string `yaml:"entity_paths"`

	Registry migrate.SchemaRegistry `yaml:"-"`
}

var (
	once      sync.Once
	config    *Config
	configErr error
)

// ==================================================
// PUBLIC API
// ==================================================

func Load(configPath ...string) (*Config, error) {
	once.Do(func() {
		cfg, err := loadConfig(configPath...)
		if err != nil {
			configErr = err
			return
		}
		config = cfg
	})
	return config, configErr
}

// GetConfig возвращает конфиг для использования в сгенерированном коде
func GetConfig() *Config {
	config, _ := Load()
	return config
}

func (c *Config) GetDSN() string {
	if env := os.Getenv("DATABASE_DSN"); env != "" {
		return env
	}
	return c.Database.DSN
}

func (c *Config) GetMigrationsDir() string {
	return c.Migrations.Dir
}

func (c *Config) NewPool(ctx context.Context) (*pgxpool.Pool, error) {
	client, err := postgres.NewClient(ctx, postgres.PoolConfig{
		DSN:      c.GetDSN(),
		MinConns: int32(c.Database.MinConnections),
		MaxConns: int32(c.Database.MaxConnections),
	})
	if err != nil {
		return nil, err
	}
	return client.Pool(), nil
}

// ==================================================
// INTERNAL LOADING PIPELINE
// ==================================================

func loadConfig(configPath ...string) (*Config, error) {
	cfg := &Config{
		Database: DatabaseConfig{
			MaxConnections: 4,
			MinConnections: 1,
		},
		Migrations: MigrationsConfig{
			Dir:       "migrations",
			TableName: "schema_migrations",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		AutoRegister: false,
	}

	path := getConfigPath(configPath...)
	if err := loadYAMLConfig(path, cfg); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load YAML config: %w", err)
	}

	loadEnvConfig(cfg)

	if err := initRegistry(cfg); err != nil {
		return nil, fmt.Errorf("failed to init registry: %w", err)
	}

	return cfg, nil
}

// ==================================================
// CONFIG LOCATION
// ==================================================

func getConfigPath(userPaths ...string) string {
	if len(userPaths) > 0 && userPaths[0] != "" {
		return userPaths[0]
	}

	possiblePaths := []string{
		"migrateme.yaml",
		"config/migrateme.yaml",
		filepath.Join(os.Getenv("HOME"), ".config", "migrateme", "config.yaml"),
	}

	for _, p := range possiblePaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return "migrateme.yaml"
}

func loadYAMLConfig(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("invalid YAML config: %w", err)
	}

	return nil
}

func loadEnvConfig(cfg *Config) {
	if v := os.Getenv("DATABASE_DSN"); v != "" {
		cfg.Database.DSN = v
	}
	if v := os.Getenv("MIGRATIONS_DIR"); v != "" {
		cfg.Migrations.Dir = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
}

// ==================================================
// REGISTRY + AUTO DISCOVERY
// ==================================================

func initRegistry(cfg *Config) error {
	cfg.Registry = make(migrate.SchemaRegistry)

	// Авторегистрация через кодогенерацию отключена в runtime
	// Вместо этого используется сгенерированный код в internal/migrator
	if cfg.AutoRegister {
		fmt.Println("⚠️  Auto-registration is disabled when using code generation.")
		fmt.Println("   Use 'migrateme discover' to generate registry code instead.")
	}

	return nil
}

// ==================================================
// PATH RESOLUTION WITH GLOBS
// ==================================================

func ResolveEntityPaths(patterns []string) ([]string, error) {
	var result []string

	for _, pattern := range patterns {
		expanded, err := expandPattern(pattern)
		if err != nil {
			return nil, err
		}
		result = append(result, expanded...)
	}

	// remove duplicates
	seen := map[string]bool{}
	out := make([]string, 0, len(result))
	for _, p := range result {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	return out, nil
}

func expandPattern(pattern string) ([]string, error) {
	// поддержка ** (recursive)
	if containsRecursive(pattern) {
		return expandRecursive(pattern)
	}

	return filepath.Glob(pattern)
}

// заменяет ** на regex-like рекурсию
func containsRecursive(p string) bool {
	return regexp.MustCompile(`\*\*`).MatchString(p)
}

func expandRecursive(pattern string) ([]string, error) {
	base := pattern
	for strings.Contains(base, "**") {
		base = strings.Replace(base, "**", "*", 1)
	}

	// начальная точка
	root := string([]rune(pattern)[:strings.Index(pattern, "**")])

	var matches []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // ignore
		}

		// Пропускаем скрытые директории и vendor
		if info.IsDir() && (strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor") {
			return filepath.SkipDir
		}

		// Пропускаем тестовые файлы
		if !info.IsDir() && strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}

		ok, _ := filepath.Match(base, path)
		if ok && !info.IsDir() && strings.HasSuffix(path, ".go") {
			matches = append(matches, path)
		}

		return nil
	})

	return matches, err
}

// ==================================================
// MANUAL REGISTRATION API (для обратной совместимости)
// ==================================================

// RegisterEntity регистрирует сущность вручную (для обратной совместимости)
func (c *Config) RegisterEntity(tableName string, entity interface{}) {
	c.Registry[tableName] = func(table string) migrate.TableSchema {
		return buildSchemaFromEntity(entity, table)
	}
}

// buildSchemaFromEntity создает схему таблицы из Go структуры
func buildSchemaFromEntity(entity interface{}, tableName string) migrate.TableSchema {
	// Эта функция теперь используется только для ручной регистрации
	// В кодогенерации используется schema.BuildSchema напрямую

	// Импортируем reflect для совместимости
	imports := map[string]string{
		"reflect": "reflect",
		"schema":  "github.com/amr0ny/migrateme/internal/infrastructure/postgres/schema",
	}
	_ = imports // временно для компиляции

	// Временная заглушка - в реальной реализации здесь должна быть рефлексия
	return migrate.TableSchema{
		TableName: tableName,
		Columns:   []migrate.ColumnMeta{},
	}
}
