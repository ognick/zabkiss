package sqlite

// Security tests: verify that the storage layer cannot be bypassed via SQL injection.

import (
	"context"
	"testing"

	"github.com/ognick/zabkiss/internal/domain"
)

// Classic SQL injection payloads must not bypass token authentication.
// All payloads must return nil (no user), never a real user record.
func TestSecurity_SQLInjection_BypassAttempts(t *testing.T) {
	repo := newTestRepo(t)

	// Seed a legitimate user that an attacker would want to impersonate.
	legit := domain.User{ID: "real-user", Name: "Настоящий", Email: "r@home.ru", Token: "secret-token"}
	if err := repo.Upsert(context.Background(), legit); err != nil {
		t.Fatal(err)
	}

	payloads := []struct {
		name  string
		token string
	}{
		{"OR true", "' OR '1'='1"},
		{"OR 1=1 comment", "' OR 1=1--"},
		{"token prefix with OR", "secret-token' OR '1'='1"},
		{"UNION SELECT", "' UNION SELECT user_id, name, email, token FROM users --"},
		{"UNION SELECT tokens", "x' UNION SELECT token, user_id, user_id, token FROM tokens --"},
		{"always-true subquery", "' OR (SELECT COUNT(*) FROM users) > 0 --"},
	}

	for _, tc := range payloads {
		t.Run(tc.name, func(t *testing.T) {
			user, err := repo.GetByToken(context.Background(), tc.token)
			if err != nil {
				// An error is acceptable — injection caused a DB error, not a bypass.
				return
			}
			if user != nil {
				t.Errorf("SQL injection bypassed auth: payload %q returned user %+v", tc.token, user)
			}
		})
	}
}

// DROP TABLE / DELETE injection must not destroy the schema.
// After any injection attempt the database must remain fully operational.
func TestSecurity_SQLInjection_SchemaIntact(t *testing.T) {
	repo := newTestRepo(t)

	destructive := []string{
		"'; DROP TABLE users; --",
		"'; DROP TABLE tokens; --",
		"x'; DELETE FROM users WHERE '1'='1",
		"x'; DELETE FROM tokens WHERE '1'='1",
		"'; INSERT INTO users (user_id,name,email) VALUES ('injected','x','x'); --",
	}
	for _, payload := range destructive {
		// Ignore result — the point is that the call must not destroy the schema.
		repo.GetByToken(context.Background(), payload) //nolint:errcheck
	}

	// Schema must still work: insert and retrieve a legitimate user.
	survivor := domain.User{ID: "survivor", Name: "Живой", Email: "s@home.ru", Token: "ok-token"}
	if err := repo.Upsert(context.Background(), survivor); err != nil {
		t.Fatalf("schema was corrupted by injection attempt: %v", err)
	}
	got, err := repo.GetByToken(context.Background(), "ok-token")
	if err != nil || got == nil || got.ID != "survivor" {
		t.Fatalf("schema was corrupted by injection attempt: got %+v, err %v", got, err)
	}
}

// Token lookup must be exact — partial tokens, wildcards, and case variants
// must never match a stored token.
func TestSecurity_TokenLookup_ExactMatchOnly(t *testing.T) {
	repo := newTestRepo(t)

	user := domain.User{ID: "u1", Name: "Иван", Email: "i@home.ru", Token: "AbCdEf-1234"}
	if err := repo.Upsert(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	nearMisses := []struct {
		name  string
		token string
	}{
		{"prefix only", "AbCdEf-123"},
		{"extended", "AbCdEf-12345"},
		{"case folded lower", "abcdef-1234"},
		{"case folded upper", "ABCDEF-1234"},
		{"SQL LIKE wildcard %", "%AbCdEf%"},
		{"SQL single-char wildcard _", "AbCdEf_1234"},
		{"leading space", " AbCdEf-1234"},
		{"trailing space", "AbCdEf-1234 "},
	}

	for _, tc := range nearMisses {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.GetByToken(context.Background(), tc.token)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != nil {
				t.Errorf("near-miss token %q matched a stored user (want nil)", tc.token)
			}
		})
	}
}

// Malicious payloads stored as user data must be treated as inert literal
// strings, not executed as SQL. The schema must survive and the data must
// be retrievable only by the exact token.
func TestSecurity_MaliciousPayload_StoredSafely(t *testing.T) {
	repo := newTestRepo(t)

	maliciousToken := "'; DROP TABLE tokens; --"
	attacker := domain.User{
		ID:    "' OR '1'='1",
		Name:  "<script>alert(document.cookie)</script>",
		Email: "x@x.ru'; DROP TABLE users; --",
		Token: maliciousToken,
	}
	if err := repo.Upsert(context.Background(), attacker); err != nil {
		t.Fatalf("Upsert rejected malicious payload (may have been executed as SQL): %v", err)
	}

	// Must be retrievable only by the exact malicious token.
	got, err := repo.GetByToken(context.Background(), maliciousToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("stored user not found by exact token — data was corrupted")
	}
	if got.ID != attacker.ID || got.Name != attacker.Name {
		t.Errorf("stored data corrupted: got %+v, want %+v", got, attacker)
	}

	// Generic lookup must NOT return the malicious user.
	nobody, err := repo.GetByToken(context.Background(), "any-other-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nobody != nil {
		t.Errorf("stored injection payload returned a user for unrelated token: %+v", nobody)
	}

	// Schema must still be intact after storing the injection payload.
	clean := domain.User{ID: "clean", Name: "Чистый", Email: "c@home.ru", Token: "clean-token"}
	if err := repo.Upsert(context.Background(), clean); err != nil {
		t.Fatalf("schema was destroyed by stored injection payload: %v", err)
	}
}
