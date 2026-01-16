package config

import (
	"context"
	"fmt"
	"github.com/amr0ny/migrateme/internal/database"
	"github.com/amr0ny/migrateme/pkg/discovery"
	"github.com/amr0ny/migrateme/pkg/migrate"
	"github.com/amr0ny/migrateme/pkg/schema"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

type DatabaseConfig struct {
	DSN string `yaml:"dsn" env:"DATABASE_DSN"`
}

type MigrationsConfig struct {
	Dir       string `yaml:"dir" env:"MIGRATIONS_DIR"`
	TableName string `yaml:"table_name" env:"MIGRATIONS_TABLE"`
}

type LoggingConfig struct {
	Level  string `yaml:"level" env:"LOG_LEVEL"`
	Format string `yaml:"format" env:"LOG_FORMAT"`
}

var (
	once      sync.Once
	config    *Config
	configErr error
)

// ==================================================
// PUBLIC API
// ==================================================

func (c *Config) GetDSN() string {
	if env := os.Getenv("DATABASE_DSN"); env != "" {
		return env
	}
	return c.Database.DSN
}

func (c *Config) GetMigrationsDir() string {
	if env := os.Getenv("MIGRATIONS_DIR"); env != "" {
		return env
	}
	return c.Migrations.Dir
}

func (c *Config) GetMigrationsTable() string {
	if env := os.Getenv("MIGRATIONS_TABLE"); env != "" {
		return env
	}
	return c.Migrations.TableName
}

func (c *Config) GetLogLevel() string {
	if env := os.Getenv("LOG_LEVEL"); env != "" {
		return env
	}
	return c.Logging.Level
}

func (c *Config) GetLogFormat() string {
	if env := os.Getenv("LOG_FORMAT"); env != "" {
		return env
	}
	return c.Logging.Format
}

func (c *Config) GetEntityPaths() []string {
	if env := os.Getenv("ENTITY_PATHS"); env != "" {
		// Используем запятую как разделитель по умолчанию
		separator := ","
		if envSeparator := os.Getenv("ENTITY_PATHS_SEPARATOR"); envSeparator != "" {
			separator = envSeparator
		}
		return strings.Split(env, separator)
	}
	return c.EntityPaths
}

func (c *Config) HasEntityPaths() bool {
	return len(c.GetEntityPaths()) > 0
}

func (c *Config) NewPool(ctx context.Context) (*pgxpool.Pool, error) {
	db, err := database.NewDB(ctx, c.GetDSN())
	if err != nil {
		return nil, err
	}
	return db.Pool, nil
}

// ==================================================
// INTERNAL LOADING PIPELINE
// ==================================================

func loadConfig(configPath ...string) (*Config, error) {
	cfg := &Config{
		Migrations: MigrationsConfig{
			Dir:       "migrations",
			TableName: "schema_migrations",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}

	path := getConfigPath(configPath...)
	if err := loadYAMLConfig(path, cfg); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load YAML config: %w", err)
	}

	loadEnvConfig(cfg)

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
	// Database config
	if v := os.Getenv("DATABASE_DSN"); v != "" {
		cfg.Database.DSN = v
	}

	// Migrations config
	if v := os.Getenv("MIGRATIONS_DIR"); v != "" {
		cfg.Migrations.Dir = v
	}
	if v := os.Getenv("MIGRATIONS_TABLE"); v != "" {
		cfg.Migrations.TableName = v
	}

	// Logging config
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}

	// Entity paths
	if v := os.Getenv("ENTITY_PATHS"); v != "" {
		separator := ","
		if envSeparator := os.Getenv("ENTITY_PATHS_SEPARATOR"); envSeparator != "" {
			separator = envSeparator
		}
		cfg.EntityPaths = strings.Split(v, separator)
	}
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
	if containsRecursive(pattern) {
		return expandRecursive(pattern)
	}

	return filepath.Glob(pattern)
}

func containsRecursive(p string) bool {
	return regexp.MustCompile(`\*\*`).MatchString(p)
}

func expandRecursive(pattern string) ([]string, error) {
	base := pattern
	for strings.Contains(base, "**") {
		base = strings.Replace(base, "**", "*", 1)
	}

	root := string([]rune(pattern)[:strings.Index(pattern, "**")])

	var matches []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // ignore
		}

		if info.IsDir() && (strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor") {
			return filepath.SkipDir
		}

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

type Config struct {
	Database   DatabaseConfig   `yaml:"database"`
	Migrations MigrationsConfig `yaml:"migrations"`
	Logging    LoggingConfig    `yaml:"logging"`

	EntityPaths []string `yaml:"entity_paths" env:"ENTITY_PATHS" envSeparator:","`

	Registry migrate.SchemaRegistry `yaml:"-"`
}

func Load(configPath ...string) (*Config, error) {
	once.Do(func() {
		cfg, err := loadConfig(configPath...)
		if err != nil {
			configErr = err
			return
		}

		// Инициализация реестра схем (опционально, только если есть entity_paths)
		if cfg.HasEntityPaths() {
			if err := initRuntimeRegistry(cfg); err != nil {
				configErr = fmt.Errorf("failed to init runtime registry: %w", err)
				return
			}
		} else {
			// Создаем пустой реестр, если нет entity_paths
			cfg.Registry = make(migrate.SchemaRegistry)
		}

		config = cfg
	})
	return config, configErr
}

func MustLoad(configPath ...string) *Config {
	cfg, err := Load(configPath...)
	if err != nil {
		panic(fmt.Sprintf("failed to load config: %v", err))
	}
	return cfg
}

func initRuntimeRegistry(cfg *Config) error {
	entityPaths := cfg.GetEntityPaths()
	if len(entityPaths) == 0 {
		// Нет путей к сущностям - это нормально, просто создаем пустой реестр
		cfg.Registry = make(migrate.SchemaRegistry)
		return nil
	}

	paths, err := ResolveEntityPaths(entityPaths)
	if err != nil {
		return fmt.Errorf("failed to resolve entity paths: %w", err)
	}

	ctx, err := discovery.LoadPackages()
	if err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}
	entities, err := discovery.DiscoverEntities(ctx, paths)
	if err != nil {
		return fmt.Errorf("failed to discover entities: %w", err)
	}

	cfg.Registry = make(migrate.SchemaRegistry)
	for _, entity := range entities {
		cfg.Registry[entity.TableName] = func(table string) migrate.TableSchema {
			return schema.BuildSchema(entity)
		}
	}

	return nil
}
