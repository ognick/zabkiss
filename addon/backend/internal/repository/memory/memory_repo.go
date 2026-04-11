package memory

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ognick/zabkiss/internal/domain"
)

// MemoryRepo хранит факты пользователей в памяти. Используется в тестах.
type MemoryRepo struct {
	mu      sync.RWMutex
	facts   map[string]map[string]string // userID → factID → text
	counter atomic.Int64
}

func NewMemoryRepo() *MemoryRepo {
	return &MemoryRepo{facts: make(map[string]map[string]string)}
}

func (r *MemoryRepo) GetFacts(_ context.Context, userID string) ([]domain.MemoryFact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := r.facts[userID]
	result := make([]domain.MemoryFact, 0, len(m))
	for id, text := range m {
		result = append(result, domain.MemoryFact{ID: id, Text: text})
	}
	return result, nil
}

func (r *MemoryRepo) AddFacts(_ context.Context, userID string, facts []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.facts[userID] == nil {
		r.facts[userID] = make(map[string]string)
	}
	for _, text := range facts {
		id := fmt.Sprintf("f%d", r.counter.Add(1))
		r.facts[userID][id] = text
	}
	return nil
}

func (r *MemoryRepo) ForgetFacts(_ context.Context, userID string, factIDs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range factIDs {
		delete(r.facts[userID], id)
	}
	return nil
}
