package repository

import (
	"context"

	"github.com/ognick/zabkiss/internal/domain"
)

type UserRepo interface {
	GetByToken(ctx context.Context, token string) (*domain.User, error)
	Upsert(ctx context.Context, user domain.User) error
}
