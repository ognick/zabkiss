package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/ognick/zabkiss/internal/domain"
)

// MemoryRepo хранит долгосрочные факты пользователей в SQLite.
type MemoryRepo struct {
	db *sql.DB
}

func NewMemoryRepo(db *sql.DB) (*MemoryRepo, error) {
	r := &MemoryRepo{db: db}
	if err := r.migrate(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *MemoryRepo) migrate() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS user_memories (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    TEXT NOT NULL,
			fact       TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, fact)
		);

		CREATE INDEX IF NOT EXISTS idx_user_memories_user_id ON user_memories(user_id);
	`)
	return err
}

func (r *MemoryRepo) GetFacts(ctx context.Context, userID string) ([]domain.MemoryFact, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, fact FROM user_memories WHERE user_id = ? ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get facts: %w", err)
	}
	defer rows.Close()

	var facts []domain.MemoryFact
	for rows.Next() {
		var id int64
		var text string
		if err := rows.Scan(&id, &text); err != nil {
			return nil, fmt.Errorf("scan fact: %w", err)
		}
		facts = append(facts, domain.MemoryFact{
			ID:   strconv.FormatInt(id, 10),
			Text: text,
		})
	}
	return facts, rows.Err()
}

func (r *MemoryRepo) AddFacts(ctx context.Context, userID string, facts []string) error {
	for _, fact := range facts {
		_, err := r.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO user_memories (user_id, fact) VALUES (?, ?)`,
			userID, fact,
		)
		if err != nil {
			return fmt.Errorf("add fact: %w", err)
		}
	}
	return nil
}

// ForgetFacts удаляет факты по их ID (не по тексту).
func (r *MemoryRepo) ForgetFacts(ctx context.Context, userID string, factIDs []string) error {
	for _, id := range factIDs {
		_, err := r.db.ExecContext(ctx,
			`DELETE FROM user_memories WHERE user_id = ? AND id = ?`,
			userID, id,
		)
		if err != nil {
			return fmt.Errorf("forget fact %s: %w", id, err)
		}
	}
	return nil
}
