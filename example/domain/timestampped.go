package domain

import "time"

type Timestamped interface {
	GetCreatedAt() time.Time
	GetUpdatedAt() time.Time
	SetCreatedAt(time.Time)
	SetUpdatedAt(time.Time)
}

type BaseTimestamped struct {
	CreatedAt time.Time `db:"created_at,type=timestamptz"`
	UpdatedAt time.Time `db:"updated_at,type=timestamptz"`
}

func NewBaseTimestamped() BaseTimestamped {
	// TODO: Обратить внимание, что здесь используется локальное время сервера
	return BaseTimestamped{
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func (t BaseTimestamped) GetCreatedAt() time.Time {
	return t.CreatedAt
}

func (t BaseTimestamped) GetUpdatedAt() time.Time {
	return t.UpdatedAt
}

func (t BaseTimestamped) SetCreatedAt(createdAt time.Time) {
	t.CreatedAt = createdAt
}
func (t BaseTimestamped) SetUpdatedAt(updatedAt time.Time) {
	t.UpdatedAt = updatedAt
}
