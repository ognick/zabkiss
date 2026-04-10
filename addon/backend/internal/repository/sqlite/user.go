package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ognick/zabkiss/internal/domain"
)

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

		CREATE TABLE IF NOT EXISTS tokens (
			token      TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(user_id),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_tokens_user_id ON tokens(user_id);
	`)
	return err
}

func (r *UserRepo) GetByToken(ctx context.Context, token string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT u.user_id, u.name, u.email, t.token
		 FROM users u
		 JOIN tokens t ON t.user_id = u.user_id
		 WHERE t.token = ?`,
		token,
	)

	var u domain.User
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Token); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get user by token: %w", err)
	}
	return &u, nil
}

func (r *UserRepo) Upsert(ctx context.Context, user domain.User) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
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

	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO tokens (token, user_id)
		VALUES (?, ?)
	`, user.Token, user.ID)
	if err != nil {
		return fmt.Errorf("insert token: %w", err)
	}

	return tx.Commit()
}
