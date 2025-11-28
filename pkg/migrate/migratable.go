package migrate

// Migratable интерфейс для сущностей, участвующих в миграциях
type Migratable interface {
	// TableName возвращает имя таблицы в БД
	TableName() string
	// IsMigratable маркерный метод для идентификации мигрируемых сущностей
	IsMigratable() bool
}

// BaseMigratable базовая реализация Migratable
type BaseMigratable struct {
	tableName string
}

// NewBaseMigratable создает новую базовую мигрируемую сущность
func NewBaseMigratable(tableName string) BaseMigratable {
	return BaseMigratable{
		tableName: tableName,
	}
}

// TableName возвращает имя таблицы
func (b BaseMigratable) TableName() string {
	return b.tableName
}

// IsMigratable всегда возвращает true для маркировки
func (b BaseMigratable) IsMigratable() bool {
	return true
}
