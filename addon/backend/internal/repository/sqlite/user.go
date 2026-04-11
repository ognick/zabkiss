package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ognick/zabkiss/internal/domain"
)

// UserRepo сохраняет профили пользователей в SQLite.
// Хранение токенов вынесено в memory.UserRepo.
type UserRepo struct {
	db *sql.DB
}

func NewUserRepo(db *sql.DB) (*UserRepo, error) {
	r := &UserRepo{db: db}
	if err := r.migrate(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *UserRepo) migrate() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			user_id    TEXT PRIMARY KEY,
			name       TEXT NOT NULL DEFAULT '',
			email      TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	return err
}

// Upsert сохраняет профиль пользователя (имя, email) в базе.
func (r *UserRepo) Upsert(ctx context.Context, user domain.User) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, email, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			name       = excluded.name,
			email      = excluded.email,
			updated_at = CURRENT_TIMESTAMP
	`, user.ID, user.Name, user.Email)
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

// GetByID возвращает пользователя по его ID.
func (r *UserRepo) GetByID(ctx context.Context, userID string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT user_id, name, email FROM users WHERE user_id = ?`, userID)
	var u domain.User
	if err := row.Scan(&u.ID, &u.Name, &u.Email); err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &u, nil
}
