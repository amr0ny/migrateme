# MigrateMe

![Go Version](https://img.shields.io/github/go-mod/go-version/amr0ny/migrateme)
![License](https://img.shields.io/github/license/amr0ny/migrateme)
![Release](https://img.shields.io/github/v/release/amr0ny/migrateme)

A intelligent database migration tool for Go that generates migrations automatically from your structs. Stop writing migrations by hand and let your code define your schema.

## 🚀 Features

- **Auto-magic Migrations** - Generate migrations directly from Go structs
- **Dependency Intelligence** - Automatically resolves and orders dependencies
- **Smart Rollbacks** - Safe down migrations with proper dependency handling
- **PostgreSQL Native** - Built on pgx for maximum performance
- **Entity Discovery** - Automatically finds migratable entities in your codebase
- **Dry-run Mode** - Preview changes before applying
- **Transactional Safety** - All migrations run in transactions
- **Flexible Config** - YAML, environment variables, and code-based configuration

## 📦 Installation

```bash
# Using go install
go install github.com/amr0ny/migrateme@latest

# From source
git clone https://github.com/amr0ny/migrateme
cd migrateme && go build -o migrateme ./cmd/migrateme
```

## ⚡ Quick Start

1. **Define your entities**:
```go
package main

import "github.com/amr0ny/migrateme/internal/domain"

type User struct {
    domain.BaseMigratable
    ID    int    `db:"id,pk"`
    Name  string `db:"name"`
    Email string `db:"email,unique"`
}

type Post struct {
    domain.BaseMigratable  
    ID     int    `db:"id,pk"`
    Title  string `db:"title"`
    UserID int    `db:"user_id,fk=users.id,delete=cascade"`
}
```

2. **Create config** (`migrateme.yaml`):
```yaml
database:
  dsn: "postgres://user:pass@localhost:5432/mydb"

migrations:
  dir: "migrations"

auto_register: true
entity_paths:
  - "**/domain/*.go"
  - "internal/models/*.go"
```

3. **Run migrations**:
```bash
# Discover your entities
migrateme discover

# Generate migrations based on schema changes
migrateme generate

# Apply migrations
migrateme run

# Check status
migrateme status

# Rollback if needed
migrateme rollback 1
```

## 🛠 Commands

| Command | Description |
|---------|-------------|
| `migrateme discover` | Find migratable entities in your codebase |
| `migrateme generate` | Generate migrations from schema differences |
| `migrateme run` | Apply all pending migrations |
| `migrateme status` | Show applied and pending migrations |
| `migrateme rollback <n>` | Rollback last N migrations |
| `migrateme create <name>` | Create empty migration template |

## 🔧 Configuration

### YAML Configuration
```yaml
database:
  dsn: "postgres://user:pass@localhost:5432/dbname"
  max_connections: 10
  min_connections: 2

migrations:
  dir: "migrations"
  table_name: "schema_migrations"

logging:
  level: "info"  # debug, info, warn, error
  format: "text" # text, json

auto_register: true
entity_paths:
  - "internal/domain/**/*.go"
  - "pkg/entities/*.go"
```

### Environment Variables
- `DATABASE_DSN` - Database connection string
- `MIGRATIONS_DIR` - Migrations directory (default: "migrations")
- `LOG_LEVEL` - Log level (default: "info")

## 🎯 Advanced Usage

### Custom Migration Names
```bash
migrateme generate "add_user_profile"
# Creates: 20240115120000__add_user_profile__a1b2c3.up.sql
```

### Dry-run Mode
```bash
migrateme generate --dry-run
# Shows what would be created without writing files
```

### Complex Entity Relationships
```go
type User struct {
    domain.BaseMigratable
    ID       int       `db:"id,pk"`
    CreatedAt time.Time `db:"created_at,default=now()"`
    Profile  *Profile  `db:"profile_id,fk=profiles.id"`
}

type Profile struct {
    domain.BaseMigratable  
    ID     int    `db:"id,pk"`
    Bio    string `db:"bio,type=text"`
    UserID int    `db:"user_id,unique"`
}
```

### Manual Schema Registration
```go
import "github.com/amr0ny/migrateme/internal/config"

cfg.RegisterEntity("custom_table", func(table string) schema.TableSchema {
    return schema.TableSchema{
        TableName: table,
        Columns: []schema.ColumnMeta{
            {
                ColumnName: "id",
                Attrs: schema.ColumnAttributes{
                    PgType:  "serial",
                    IsPK:    true,
                    NotNull: true,
                },
            },
            {
                ColumnName: "data", 
                Attrs: schema.ColumnAttributes{
                    PgType: "jsonb",
                },
            },
        },
    }
})
```

## 🔍 How It Works

1. **Discovery Phase** - Scans your code for structs with `BaseMigratable`
2. **Schema Analysis** - Compares current DB schema with code definitions
3. **Dependency Graph** - Builds dependency graph for foreign keys
4. **Topological Sort** - Orders migrations to satisfy dependencies
5. **SQL Generation** - Creates safe, transactional migration SQL
6. **Execution** - Applies migrations in correct order

## 🛡 Safety Features

- **Transaction Wrapping** - Every migration runs in a transaction
- **Dependency Validation** - Detects and reports circular dependencies
- **Safe Rollbacks** - Down migrations preserve data integrity
- **Constraint Handling** - Smart handling of NOT NULL constraints
- **Dry-run Mode** - Preview changes before execution

## 🤝 Contributing

We love contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a PR

## 📄 License

MIT License - see [LICENSE](LICENSE) for details.

## 🆘 Support

- 📖 [Documentation](https://github.com/amr0ny/migrateme/docs)
- 🐛 [Issue Tracker](https://github.com/amr0ny/migrateme/issues)
- 💬 [Discussions](https://github.com/amr0ny/migrateme/discussions)

---

**MigrateMe** - Because your database schema should evolve as gracefully as your code.