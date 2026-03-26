# MigrateMe

![Go Version](https://img.shields.io/github/go-mod/go-version/amr0ny/migrateme)
![License](https://img.shields.io/github/license/amr0ny/migrateme)

> 🇷🇺 [Русская версия](README.ru.md)

A code-first database migration tool for Go that automatically generates migrations from your structs. Stop writing SQL by hand — let your code define the schema.

## 🚀 Features

- **Automatic migrations** — Generate migrations directly from Go structs
- **Smart dependency resolution** — Automatically resolves and orders FK dependencies
- **Safe rollbacks** — Down-migrations preserve referential integrity
- **Multi-dialect foundations** — PostgreSQL and SQLite are supported via dialect adapters
- **Entity discovery** — Automatically finds migratable structs in your codebase
- **Dry-run mode** — Preview changes before applying them
- **Transactional safety** — Every migration runs inside a transaction
- **Flexible configuration** — YAML, environment variables, or code

## 📦 Installation

```bash
# Using go install
go install github.com/amr0ny/migrateme@latest

# From source
git clone https://github.com/amr0ny/migrateme
cd migrateme && go build -o migrateme ./cmd/migrateme
```

## ⚡ Quick Start

### 1. Define your entities

```go
package domain

// table: users
type User struct {
    ID        int       `db:"id,pk"`
    Name      string    `db:"name"`
    Email     string    `db:"email,unique"`
    CreatedAt time.Time `db:"created_at,default=now()"`
}

// table: posts
type Post struct {
    ID      int    `db:"id,pk"`
    Title   string `db:"title"`
    UserID  int    `db:"user_id,fk=users.id,delete=cascade"`
    Content string `db:"content,type=text"`
}
```

### 2. Create a config file

Create `migrateme.yaml`:

```yaml
database:
  dialect: "postgres" # postgres | sqlite
  dsn: "postgres://user:pass@localhost:5432/mydb"

migrations:
  dir: "migrations"

entity_paths:
  - "internal/domain/**/*.go"
  - "pkg/models/*.go"
```

### 3. Run migrations

```bash
# Generate migrations from schema diff
migrateme generate

# Apply pending migrations
migrateme run

# Check status
migrateme status

# Roll back if needed
migrateme rollback 1
```

## 🛠 Commands

| Command | Description |
|---------|-------------|
| `migrateme generate [name]` | Generate migrations from schema diff |
| `migrateme run` | Apply all pending migrations |
| `migrateme status` | Show applied and pending migrations |
| `migrateme rollback <n>` | Roll back the last N migrations |
| `migrateme create <name>` | Create an empty migration template |

## 🔧 Configuration

### YAML

```yaml
database:
  dialect: "postgres" # postgres | sqlite
  dsn: "postgres://user:pass@localhost:5432/dbname"

migrations:
  dir: "migrations"
  table_name: "schema_migrations"

logging:
  level: "info"  # debug, info, warn, error
  format: "text" # text, json

entity_paths:
  - "internal/domain/**/*.go"
  - "pkg/entities/*.go"
```

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_DIALECT` | `postgres` | Database dialect (`postgres`, `sqlite`) |
| `DATABASE_DSN` | — | Database connection string |
| `MIGRATIONS_DIR` | `migrations` | Migrations directory |
| `LOG_LEVEL` | `info` | Log level |

### Dialect examples

```yaml
# PostgreSQL
database:
  dialect: "postgres"
  dsn: "postgres://user:pass@localhost:5432/app"
```

```yaml
# SQLite (file)
database:
  dialect: "sqlite"
  dsn: "./app.db"
```

## 🎯 Advanced Usage

### Custom migration names

```bash
migrateme generate "add_user_profile"
# Creates: 20240115120000__add_user_profile__a1b2c3.up.sql
```

### Dry-run mode

```bash
migrateme generate --dry-run
# Shows what would be created without writing any files
```

### Complex entity relationships

```go
// table: users
type User struct {
    ID        int       `db:"id,pk"`
    CreatedAt time.Time `db:"created_at,default=now()"`
    ProfileID int       `db:"profile_id,fk=profiles.id"`
}

// table: profiles
type Profile struct {
    ID     int    `db:"id,pk"`
    Bio    string `db:"bio,type=text"`
    UserID int    `db:"user_id,unique,fk=users.id"`
}
```

### Rich data type support

```go
// table: products
type Product struct {
    ID        uuid.UUID       `db:"id,pk,type=uuid"`
    Name      string          `db:"name"`
    Price     decimal.Decimal `db:"price,type=numeric(10,2)"`
    Tags      []string        `db:"tags,type=jsonb"`
    Metadata  map[string]any  `db:"metadata,type=jsonb"`
    IsActive  bool            `db:"is_active,default=true"`
    CreatedAt time.Time       `db:"created_at,default=now()"`
    UpdatedAt *time.Time      `db:"updated_at"`
}
```

## 🏗 Project Structure

```
migrateme/
├── cmd/migrateme/
│   └── main.go                 # CLI entry point
├── internal/
│   ├── cli/                    # CLI command implementations
│   ├── core/                   # Core migration logic
│   └── database/               # Database connection handling
├── example/
│   └── domain/                 # Example domain models
├── pkg/
│   ├── config/                 # Configuration management
│   ├── discovery/              # Entity discovery (AST parsing)
│   ├── migrate/                # Migration types and interfaces
│   └── schema/                 # Schema diffing and management
├── migrations/                 # Generated migration files
└── migrateme.yaml              # Configuration file
```

## 🔍 How It Works

1. **Discovery** — Scans your codebase for structs annotated with `// table: <name>`
2. **Schema analysis** — Compares the current database schema against struct definitions
3. **Dependency graph** — Builds a directed graph of foreign key relationships
4. **Topological sort** — Orders migrations to satisfy all dependencies
5. **SQL generation** — Produces safe, transactional SQL for each change
6. **Execution** — Applies migrations in the correct order

## 🛡 Safety

- **Transaction wrapping** — Each migration runs atomically inside a transaction
- **Cycle detection** — Circular FK dependencies are detected and reported before execution
- **Safe rollbacks** — Down-migrations respect referential integrity
- **Constraint handling** — NOT NULL constraints are handled gracefully during alterations
- **Dry-run mode** — Inspect generated SQL before touching the database

## 📋 Requirements

- Go 1.24+
- PostgreSQL 12+ (for postgres dialect)
- SQLite 3+ (for sqlite dialect)

## 🧭 Dialect support matrix

| Feature | PostgreSQL | SQLite |
|---------|------------|--------|
| Create table | ✅ | ✅ |
| Add column | ✅ | ✅ |
| Drop/alter column | ✅ | ✅ (auto-rebuild plan) |
| Primary/unique constraints | ✅ | ✅ (auto-rebuild plan) |
| Foreign keys | ✅ | ✅ (direct + auto-rebuild) |
| Indexes | ✅ | ✅ |
| Check constraints | ✅ | ✅ (direct + auto-rebuild) |
| Deterministic diff ordering | ✅ | ✅ |

For SQLite, incompatible `ALTER TABLE` changes are planned as deterministic table rebuilds (create temp table, copy data, swap, recreate indexes). The generator also emits compatibility notes for best-effort mappings such as `uuid/jsonb/timestamptz -> TEXT` and `boolean -> INTEGER`.

## 🎨 Struct Tag Reference

### Basic attributes

```go
type Example struct {
    ID    int    `db:"id,pk"`        // Primary key
    Name  string `db:"name,notnull"` // NOT NULL
    Email string `db:"email,unique"` // Unique constraint
}
```

### Data types

```go
type Example struct {
    Price decimal.Decimal `db:"price,type=numeric(10,2)"`
    Data  map[string]any  `db:"data,type=jsonb"`
    Tags  []string        `db:"tags,type=jsonb"`
    UUID  uuid.UUID       `db:"uuid,type=uuid"`
}
```

### Foreign keys

```go
type Example struct {
    UserID int `db:"user_id,fk=users.id,delete=cascade,update=restrict"`
}
```

### Indexes (via doc comments)

Composite and partial indexes are declared as struct-level directives in the doc comment above the `type`:

```go
// table: posts
// index: idx_posts_user_id_created_at(user_id, created_at)
// index: unique idx_posts_slug(slug)
// index: idx_posts_active_user(user_id) where deleted_at IS NULL
type Post struct {
    UserID    int       `db:"user_id"`
    CreatedAt time.Time `db:"created_at"`
    Slug      string    `db:"slug"`
}
```

Syntax:
- `// index: [unique ]<idx_name>(col1, col2, ...)`
- Name is optional: `// index: (col1, col2)` — a name will be generated
- Partial index: `// index: <idx_name>(col1, ...) where <predicate>`

### CHECK constraints (via doc comments)

```go
// table: products
// check: chk_price_positive(price > 0)
// check: (qty >= 0)
type Product struct {
    Price int `db:"price"`
    Qty   int `db:"qty"`
}
```

Syntax:
- `// check: <chk_name>(<expr>)`
- Name is optional: `// check: (<expr>)` — a name will be generated

### Default values

```go
type Example struct {
    CreatedAt time.Time `db:"created_at,default=now()"`
    IsActive  bool      `db:"is_active,default=true"`
    Version   int       `db:"version,default=1"`
}
```

## 🤝 Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting a pull request.

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Submit a pull request

## 📄 License

MIT License — see [LICENSE](LICENSE) for details.

## 🆘 Support

- 📖 Documentation: this README and `migrateme --help`
- 🐛 [Issue tracker](https://github.com/amr0ny/migrateme/issues)
- 💬 [Discussions](https://github.com/amr0ny/migrateme/discussions)

---

**MigrateMe** — Because your database schema should evolve as elegantly as your code.