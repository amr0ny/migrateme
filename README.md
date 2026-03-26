# MigrateMe

![Go Version](https://img.shields.io/github/go-mod/go-version/amr0ny/migrateme)
![License](https://img.shields.io/github/license/amr0ny/migrateme)

Умный инструмент для миграций баз данных на Go, который автоматически генерирует миграции из ваших структур. Перестаньте писать миграции вручную и позвольте вашему коду определять схему базы данных.

## 🚀 Особенности

- **Автоматические миграции** - Генерация миграций напрямую из Go структур
- **Умные зависимости** - Автоматическое разрешение и упорядочивание зависимостей
- **Безопасные откаты** - Безопасные down-миграции с правильной обработкой зависимостей
- **Нативный PostgreSQL** - Построен на pgx для максимальной производительности
- **Обнаружение сущностей** - Автоматическое нахождение мигрируемых сущностей в вашем коде
- **Режим предпросмотра** - Просмотр изменений перед применением
- **Транзакционная безопасность** - Все миграции выполняются в транзакциях
- **Гибкая конфигурация** - YAML, переменные окружения и кодовая конфигурация

## 📦 Установка

```bash
# Используя go install
go install github.com/amr0ny/migrateme@latest

# Из исходного кода
git clone https://github.com/amr0ny/migrateme
cd migrateme && go build -o migrateme ./cmd/migrateme
```

## ⚡ Быстрый старт

### 1. Определите ваши сущности

```go
package domain

import "github.com/amr0ny/migrateme/internal/domain"

// table: users
type User struct {
    ID       int    `db:"id,pk"`
    Name     string `db:"name"`
    Email    string `db:"email,unique"`
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

### 2. Создайте конфигурацию

Создайте `migrateme.yaml`:

```yaml
database:
  dsn: "postgres://user:pass@localhost:5432/mydb"

migrations:
  dir: "migrations"

entity_paths:
  - "internal/domain/*.go"
  - "pkg/models/*.go"
```

### 3. Запустите миграции

```bash
# Сгенерировать миграции на основе изменений схемы
migrateme generate

# Применить миграции
migrateme run

# Проверить статус
migrateme status

# Откатить при необходимости
migrateme rollback 1
```

## 🛠 Команды

| Команда | Описание |
|---------|-------------|
| `migrateme generate [name]` | Сгенерировать миграции из различий схем |
| `migrateme run` | Применить все ожидающие миграции |
| `migrateme status` | Показать примененные и ожидающие миграции |
| `migrateme rollback <n>` | Откатить последние N миграций |
| `migrateme create <name>` | Создать шаблон пустой миграции |

## 🔧 Конфигурация

### YAML конфигурация

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

entity_paths:
  - "internal/domain/**/*.go"
  - "pkg/entities/*.go"
```

### Переменные окружения

- `DATABASE_DSN` - Строка подключения к базе данных
- `MIGRATIONS_DIR` - Директория миграций (по умолчанию: "migrations")
- `LOG_LEVEL` - Уровень логирования (по умолчанию: "info")

## 🎯 Продвинутое использование

### Пользовательские имена миграций

```bash
migrateme generate "add_user_profile"
# Создает: 20240115120000__add_user_profile__a1b2c3.up.sql
```

### Режим предпросмотра

```bash
migrateme generate --dry-run
# Показывает что будет создано без записи файлов
```

### Сложные связи между сущностями

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

### Поддержка различных типов данных

```go
// table: products
type Product struct {
    ID          uuid.UUID       `db:"id,pk,type=uuid"`
    Name        string          `db:"name"`
    Price       decimal.Decimal `db:"price,type=numeric(10,2)"`
    Tags        []string        `db:"tags,type=jsonb"`
    Metadata    map[string]any  `db:"metadata,type=jsonb"`
    IsActive    bool            `db:"is_active,default=true"`
    CreatedAt   time.Time       `db:"created_at,default=now()"`
    UpdatedAt   *time.Time      `db:"updated_at"`
}
```

## 🏗 Архитектура проекта

```
migrateme/
├── cmd/migrateme/
│   └── main.go                 # Точка входа CLI
├── internal/
│   ├── cli/                    # Реализации CLI команд
│   ├── core/                   # Основная логика миграций
│   ├── database/               # Работа с подключением к БД
│   └── domain/                 # Доменные модели
├── pkg/
│   ├── config/                 # Управление конфигурацией
│   ├── discovery/              # Обнаружение сущностей в коде
│   ├── migrate/                # Типы и интерфейсы миграций
│   └── schema/                 # Управление схемой БД
├── migrations/                 # Сгенерированные файлы миграций
└── migrateme.yaml             # Файл конфигурации
```

## 🔍 Как это работает

1. **Фаза обнаружения** - Сканирует ваш код на наличие структур с комментариями `table: "name"`
2. **Анализ схемы** - Сравнивает текущую схему БД с определениями в коде
3. **Граф зависимостей** - Строит граф зависимостей для внешних ключей
4. **Топологическая сортировка** - Упорядочивает миграции для удовлетворения зависимостей
5. **Генерация SQL** - Создает безопасный, транзакционный SQL для миграций
6. **Выполнение** - Применяет миграции в правильном порядке

## 🛡 Функции безопасности

- **Оборачивание в транзакции** - Каждая миграция выполняется в транзакции
- **Валидация зависимостей** - Обнаружение и отчет о циклических зависимостях
- **Безопасные откаты** - Down-миграции сохраняют целостность данных
- **Обработка ограничений** - Умная обработка NOT NULL ограничений
- **Режим предпросмотра** - Просмотр изменений перед выполнением

## 📋 Требования

- Go 1.24 или новее
- PostgreSQL 12 или новее

## 🎨 Поддерживаемые теги структур

### Базовые атрибуты
```go
type Example struct {
    ID    int    `db:"id,pk"`           // Первичный ключ
    Name  string `db:"name,notnull"`    // NOT NULL
    Email string `db:"email,unique"`    // Уникальное ограничение
}
```

### Типы данных
```go
type Example struct {
    Price decimal.Decimal `db:"price,type=numeric(10,2)"`
    Data  map[string]any  `db:"data,type=jsonb"`
    Tags  []string        `db:"tags,type=jsonb"`
    UUID  uuid.UUID       `db:"uuid,type=uuid"`
}
```

### Внешние ключи
```go
type Example struct {
    UserID int `db:"user_id,fk=users.id,delete=cascade,update=restrict"`
}
```

### Индексы (композитные) из комментариев
Поддерживаются `struct-level` директивы в doc-комментарии над `type`:

```go
// table: posts
// index: idx_posts_user_id_created_at(user_id, created_at)
// index: unique idx_posts_slug(slug)
type Post struct {
    UserID int       `db:"user_id"`
    CreatedAt time.Time `db:"created_at"`
    Slug   string    `db:"slug"`
}
```

Синтаксис:
- `// index: [unique ]<idx_name>(col1, col2, ...)`
- `<idx_name>` опционален: `// index: (col1, col2)` (будет сгенерировано имя)

### Значения по умолчанию
```go
type Example struct {
    CreatedAt time.Time `db:"created_at,default=now()"`
    IsActive  bool      `db:"is_active,default=true"`
    Version   int       `db:"version,default=1"`
}
```

## 🤝 Участие в разработке

Мы приветствуем вклад в разработку! Пожалуйста, ознакомьтесь с нашим руководством по внесению вклада.

1. Сделайте форк репозитория
2. Создайте ветку для функциональности
3. Внесите свои изменения
5. Отправьте Pull Request

## 📄 Лицензия

MIT License - смотрите [LICENSE](LICENSE) для деталей.

## 🆘 Поддержка

- 📖 [Документация](https://github.com/amr0ny/migrateme/docs)
- 🐛 [Трекер задач](https://github.com/amr0ny/migrateme/issues)
- 💬 [Обсуждения](https://github.com/amr0ny/migrateme/discussions)

---

**MigrateMe** - Потому что схема вашей базы данных должна развиваться так же элегантно, как и ваш код.
