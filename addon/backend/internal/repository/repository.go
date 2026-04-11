package repository

import (
	"context"

	"github.com/ognick/zabkiss/internal/domain"
)

type UserRepo interface {
	GetByToken(ctx context.Context, token string) (*domain.User, error)
	Upsert(ctx context.Context, user domain.User) error
}

// MemoryRepo хранит долгосрочные факты пользователя.
type MemoryRepo interface {
	GetFacts(ctx context.Context, userID string) ([]domain.MemoryFact, error)
	AddFacts(ctx context.Context, userID string, facts []string) error
	ForgetFacts(ctx context.Context, userID string, factIDs []string) error
}
