package alice

import (
	"context"

	"github.com/ognick/zabkiss/internal/domain"
)

type logEntry struct {
	msg  string
	args []any
}

type mockLogger struct {
	logged []logEntry
}

func (m *mockLogger) Info(msg string, args ...any)  {}
func (m *mockLogger) Debug(msg string, args ...any) {}
func (m *mockLogger) Warn(msg string, args ...any)  {}
func (m *mockLogger) Error(msg string, args ...any) {
	m.logged = append(m.logged, logEntry{msg: msg, args: args})
}

type mockAuth struct {
	user *domain.User
	err  error
}

func (m *mockAuth) ResolveUser(_ context.Context, _ string) (domain.User, error) {
	if m.user != nil {
		return *m.user, m.err
	}
	return domain.User{}, m.err
}

type mockUserRepo struct {
	user      *domain.User
	getErr    error
	upsertErr error
	upserted  *domain.User
}

func (m *mockUserRepo) GetByToken(_ context.Context, _ string) (*domain.User, error) {
	return m.user, m.getErr
}

func (m *mockUserRepo) Upsert(_ context.Context, user domain.User) error {
	m.upserted = &user
	return m.upsertErr
}

type mockEcho struct {
	reply string
	err   error
}

func (m *mockEcho) Say(_ context.Context, _ string, _ []string) (string, error) {
	return m.reply, m.err
}

type mockPolicy struct {
	entities []string
	err      error
}

func (m *mockPolicy) GetEntities(_ context.Context) ([]string, error) {
	return m.entities, m.err
}
