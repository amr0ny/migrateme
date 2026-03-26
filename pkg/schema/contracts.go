package schema

import (
	"context"

	"github.com/amr0ny/migrateme/pkg/migrate"
)

type TableFetcher interface {
	Fetch(ctx context.Context, table string) (migrate.TableSchema, error)
}

type SchemaDiffer interface {
	DiffSchemas(old, new migrate.TableSchema) migrate.TableDiff
}

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
)

type Diagnostic struct {
	Severity Severity
	Table    string
	Message  string
}
