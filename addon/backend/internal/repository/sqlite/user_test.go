package sqlite

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/ognick/zabkiss/internal/domain"
)

func newTestRepo(t *testing.T) *UserRepo {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	repo, err := NewUserRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestGetByToken_NotFound(t *testing.T) {
	repo := newTestRepo(t)

	user, err := repo.GetByToken(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if user != nil {
		t.Errorf("expected nil, got %+v", user)
	}
}

func TestUpsertAndGetByToken(t *testing.T) {
	repo := newTestRepo(t)
	user := domain.User{ID: "u1", Name: "Иван", Email: "ivan@example.com", Token: "tok1"}

	if err := repo.Upsert(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByToken(context.Background(), "tok1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected user, got nil")
	}
	if got.ID != "u1" {
		t.Errorf("ID: got %q, want u1", got.ID)
	}
	if got.Name != "Иван" {
		t.Errorf("Name: got %q, want Иван", got.Name)
	}
	if got.Email != "ivan@example.com" {
		t.Errorf("Email: got %q", got.Email)
	}
	if got.Token != "tok1" {
		t.Errorf("Token: got %q, want tok1", got.Token)
	}
}

func TestUpsert_UpdatesExistingUser(t *testing.T) {
	repo := newTestRepo(t)

	original := domain.User{ID: "u1", Name: "Иван", Email: "old@ya.ru", Token: "tok1"}
	if err := repo.Upsert(context.Background(), original); err != nil {
		t.Fatal(err)
	}

	updated := domain.User{ID: "u1", Name: "Иван Иванов", Email: "new@ya.ru", Token: "tok1"}
	if err := repo.Upsert(context.Background(), updated); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByToken(context.Background(), "tok1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Иван Иванов" {
		t.Errorf("Name: got %q, want Иван Иванов", got.Name)
	}
	if got.Email != "new@ya.ru" {
		t.Errorf("Email: got %q, want new@ya.ru", got.Email)
	}
}

func TestUpsert_MultipleTokensSameUser(t *testing.T) {
	repo := newTestRepo(t)

	u1 := domain.User{ID: "u1", Name: "Иван", Email: "ivan@ya.ru", Token: "tok1"}
	if err := repo.Upsert(context.Background(), u1); err != nil {
		t.Fatal(err)
	}
	u2 := domain.User{ID: "u1", Name: "Иван", Email: "ivan@ya.ru", Token: "tok2"}
	if err := repo.Upsert(context.Background(), u2); err != nil {
		t.Fatal(err)
	}

	got1, err := repo.GetByToken(context.Background(), "tok1")
	if err != nil || got1 == nil {
		t.Fatalf("tok1 lookup failed: %v", err)
	}
	got2, err := repo.GetByToken(context.Background(), "tok2")
	if err != nil || got2 == nil {
		t.Fatalf("tok2 lookup failed: %v", err)
	}
	if got1.ID != got2.ID {
		t.Error("both tokens should belong to the same user")
	}
}

func TestUpsert_DuplicateTokenIsIgnored(t *testing.T) {
	repo := newTestRepo(t)

	user := domain.User{ID: "u1", Name: "Иван", Email: "ivan@ya.ru", Token: "tok1"}
	if err := repo.Upsert(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	// Same token — INSERT OR IGNORE should not fail
	if err := repo.Upsert(context.Background(), user); err != nil {
		t.Errorf("duplicate token insert should be ignored, got: %v", err)
	}
}
