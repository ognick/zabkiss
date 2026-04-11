package sqlite

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/ognick/zabkiss/internal/domain"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestUserRepo_Upsert_CreatesUser(t *testing.T) {
	repo, err := NewUserRepo(newTestDB(t))
	if err != nil {
		t.Fatal(err)
	}

	user := domain.User{ID: "u1", Name: "Иван", Email: "ivan@example.com"}
	if err := repo.Upsert(context.Background(), user); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := repo.GetByID(context.Background(), "u1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "Иван" {
		t.Errorf("Name: got %q, want Иван", got.Name)
	}
}

func TestUserRepo_Upsert_UpdatesExistingUser(t *testing.T) {
	repo, err := NewUserRepo(newTestDB(t))
	if err != nil {
		t.Fatal(err)
	}

	repo.Upsert(context.Background(), domain.User{ID: "u1", Name: "Старое", Email: "old@ya.ru"}) //nolint:errcheck

	if err := repo.Upsert(context.Background(), domain.User{ID: "u1", Name: "Новое", Email: "new@ya.ru"}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByID(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Новое" {
		t.Errorf("Name: got %q, want Новое", got.Name)
	}
}

func TestMemoryRepo_AddAndGet(t *testing.T) {
	repo, err := NewMemoryRepo(newTestDB(t))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := repo.AddFacts(ctx, "u1", []string{"любит кофе", "не любит холод"}); err != nil {
		t.Fatal(err)
	}

	facts, err := repo.GetFacts(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Errorf("want 2 facts, got %d: %v", len(facts), facts)
	}
}

func TestMemoryRepo_ForgetFacts(t *testing.T) {
	repo, err := NewMemoryRepo(newTestDB(t))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	repo.AddFacts(ctx, "u1", []string{"любит кофе", "любит чай"}) //nolint:errcheck

	// Получаем факты чтобы узнать ID
	facts, err := repo.GetFacts(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	// Находим ID факта "любит кофе"
	var coffeeID string
	for _, f := range facts {
		if f.Text == "любит кофе" {
			coffeeID = f.ID
		}
	}
	if coffeeID == "" {
		t.Fatal("fact 'любит кофе' not found")
	}

	repo.ForgetFacts(ctx, "u1", []string{coffeeID}) //nolint:errcheck

	remaining, err := repo.GetFacts(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].Text != "любит чай" {
		t.Errorf("want [любит чай], got %v", remaining)
	}
}

func TestMemoryRepo_DuplicateFactIgnored(t *testing.T) {
	repo, err := NewMemoryRepo(newTestDB(t))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	repo.AddFacts(ctx, "u1", []string{"любит кофе"}) //nolint:errcheck
	repo.AddFacts(ctx, "u1", []string{"любит кофе"}) //nolint:errcheck

	facts, err := repo.GetFacts(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Errorf("want 1 fact (dedup), got %d: %v", len(facts), facts)
	}
}
