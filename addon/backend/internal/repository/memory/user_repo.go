package memory

import (
	"context"
	"sync"

	"github.com/ognick/zabkiss/internal/domain"
)

// UserRepo хранит отображение token→User в памяти.
// Токены считаются временными данными и не переживают перезапуск.
type UserRepo struct {
	mu    sync.RWMutex
	store map[string]domain.User // key = token
}

func NewUserRepo() *UserRepo {
	return &UserRepo{store: make(map[string]domain.User)}
}

func (r *UserRepo) GetByToken(_ context.Context, token string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.store[token]
	if !ok {
		return nil, nil
	}
	cp := u
	return &cp, nil
}

func (r *UserRepo) Upsert(_ context.Context, user domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store[user.Token] = user
	return nil
}
