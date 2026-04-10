package alice

// Security tests: verify that an unauthenticated or unauthorized attacker
// cannot execute commands or extract internal information from the system.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ognick/zabkiss/internal/domain"
)

// ── Partial credentials ──────────────────────────────────────────────────────

// Attack: send a valid token but omit user_id (no OAuth linked on Alice side).
func TestSecurity_OnlyToken_NoUserID_Rejected(t *testing.T) {
	h := &Handler{log: &mockLogger{}, echoSrv: &mockEcho{reply: "не должно выполниться"}, auth: &mockAuth{}}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"","access_token":"valid-tok"}}}`
	var resp aliceResponse
	mustDecode(t, postWebhook(t, h, body), &resp)
	assertAccountLinking(t, resp)
}

// Attack: send a user_id but omit the OAuth token.
func TestSecurity_OnlyUserID_NoToken_Rejected(t *testing.T) {
	h := &Handler{log: &mockLogger{}, echoSrv: &mockEcho{reply: "не должно выполниться"}, auth: &mockAuth{}}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"u1","access_token":""}}}`
	var resp aliceResponse
	mustDecode(t, postWebhook(t, h, body), &resp)
	assertAccountLinking(t, resp)
}

// ── Command gate ─────────────────────────────────────────────────────────────

// Auth failure must block command execution: echoSrv.Say must never be called.
func TestSecurity_EchoNotCalledOnAuthFailure(t *testing.T) {
	const sentinel = "КОМАНДА_БЕЗ_АВТОРИЗАЦИИ"
	h := &Handler{
		log:     &mockLogger{},
		echoSrv: &mockEcho{reply: sentinel},
		auth:    &mockAuth{err: errors.New("db down")},
	}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"u1","access_token":"tok"}},"request":{"command":"включи весь свет"}}`
	w := postWebhook(t, h, body)

	var resp aliceResponse
	mustDecode(t, w, &resp)
	if resp.Response.Text == sentinel {
		t.Error("command executed despite failed authentication")
	}
}

// Email block must also prevent command execution.
func TestSecurity_EchoNotCalledOnEmailBlock(t *testing.T) {
	const sentinel = "КОМАНДА_БЕЗ_ДОСТУПА"
	user := &domain.User{ID: "u1", Name: "Злоумышленник", Email: "bad@evil.com"}
	h := &Handler{
		log:           &mockLogger{},
		echoSrv:       &mockEcho{reply: sentinel},
		auth:          &mockAuth{user: user},
		allowedEmails: []string{"trusted@home.ru"},
	}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"u1","access_token":"tok"}},"request":{"command":"открой замок"}}`
	w := postWebhook(t, h, body)

	var resp aliceResponse
	mustDecode(t, w, &resp)
	if resp.Response.Text == sentinel {
		t.Error("command executed despite email not being in allowlist")
	}
}

// ── Error isolation ───────────────────────────────────────────────────────────

// Internal error messages (DB address, stack trace fragments) must never
// reach the client — attacker must not learn anything about infrastructure.
func TestSecurity_InternalAuthError_NotLeakedToClient(t *testing.T) {
	internalMsg := "dial tcp 10.0.0.5:5432: connection refused"
	h := &Handler{
		log:  &mockLogger{},
		auth: &mockAuth{err: errors.New(internalMsg)},
	}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"u1","access_token":"tok"}}}`
	w := postWebhook(t, h, body)

	var resp aliceResponse
	mustDecode(t, w, &resp)
	if strings.Contains(resp.Response.Text, "10.0.0.5") ||
		strings.Contains(resp.Response.Text, "connection refused") ||
		strings.Contains(resp.Response.Text, internalMsg) {
		t.Errorf("internal infrastructure details leaked to client: %q", resp.Response.Text)
	}
	assertAccountLinking(t, resp)
}

// ── Email allowlist (isAllowed) ───────────────────────────────────────────────

func TestSecurity_IsAllowed(t *testing.T) {
	tests := []struct {
		name          string
		allowedEmails []string
		email         string
		wantAllowed   bool
	}{
		{
			name:          "nil allowlist blocks all",
			allowedEmails: nil,
			email:         "anyone@evil.com",
			wantAllowed:   false,
		},
		{
			name:          "empty allowlist blocks all",
			allowedEmails: []string{},
			email:         "anyone@evil.com",
			wantAllowed:   false,
		},
		{
			name:          "email in list",
			allowedEmails: []string{"alice@home.ru", "bob@home.ru"},
			email:         "alice@home.ru",
			wantAllowed:   true,
		},
		{
			name:          "email not in list",
			allowedEmails: []string{"alice@home.ru"},
			email:         "evil@attacker.com",
			wantAllowed:   false,
		},
		{
			// Attacker tries "ALICE@HOME.RU" when allowlist has "alice@home.ru".
			name:          "case mismatch rejected",
			allowedEmails: []string{"alice@home.ru"},
			email:         "ALICE@HOME.RU",
			wantAllowed:   false,
		},
		{
			// Attacker registers "alice@home.ru.evil.com" to match "alice@home.ru".
			name:          "domain suffix not confused with subdomain",
			allowedEmails: []string{"alice@home.ru"},
			email:         "alice@home.ru.evil.com",
			wantAllowed:   false,
		},
		{
			// Attacker uses email that is a prefix of an allowed email.
			name:          "prefix match rejected",
			allowedEmails: []string{"alice@home.ru"},
			email:         "alice@home.r",
			wantAllowed:   false,
		},
		{
			// Attacker uses whitespace padding to fool naive comparison.
			name:          "leading space rejected",
			allowedEmails: []string{"alice@home.ru"},
			email:         " alice@home.ru",
			wantAllowed:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{allowedEmails: tc.allowedEmails}
			if got := h.isAllowed(tc.email); got != tc.wantAllowed {
				t.Errorf("isAllowed(%q) = %v, want %v", tc.email, got, tc.wantAllowed)
			}
		})
	}
}

// Verified email that is not in the allowlist gets a denial message — not a
// system error, not an account-linking directive.
func TestSecurity_AllowlistDenial_CorrectResponse(t *testing.T) {
	user := &domain.User{ID: "u1", Name: "Иван", Email: "ivan@other.com"}
	h := &Handler{
		log:           &mockLogger{},
		echoSrv:       &mockEcho{},
		auth:          &mockAuth{user: user},
		allowedEmails: []string{"owner@home.ru"},
	}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"u1","access_token":"tok"}}}`
	w := postWebhook(t, h, body)

	var resp aliceResponse
	mustDecode(t, w, &resp)
	// Must mention the user's name (personalised denial), not a generic auth error.
	if !strings.Contains(resp.Response.Text, "Иван") {
		t.Errorf("denial message should include user name, got: %q", resp.Response.Text)
	}
	// Must NOT start account linking — user IS authenticated, just not authorised.
	if resp.Response.Directives != nil && resp.Response.Directives.StartAccountLinking != nil {
		t.Error("account linking should not be triggered for authorised-but-denied user")
	}
}

// ── YandexAuth: fail-closed behaviour ────────────────────────────────────────

// Attack: present a token that Yandex rejects with HTTP 401.
// System must fail closed — not silently grant access.
func TestSecurity_YandexNon200_FailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "OAuth token is invalid or expired"})
	}))
	defer srv.Close()

	a := NewAuth(&mockUserRepo{})
	a.httpClient = testClient(srv.URL)

	_, err := a.ResolveUser(context.Background(), "stolen-or-expired-token")
	if err == nil {
		t.Error("Yandex 401 must be rejected, not silently allowed")
	}
}

// Attack: Yandex API is completely unreachable (network error).
// System must fail closed — not grant access because it "can't verify".
func TestSecurity_YandexUnreachable_FailsClosed(t *testing.T) {
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close() // shut down immediately to simulate unreachable endpoint

	a := NewAuth(&mockUserRepo{})
	a.httpClient = testClient(url)

	_, err := a.ResolveUser(context.Background(), "some-token")
	if err == nil {
		t.Error("unreachable Yandex API must be rejected, not silently allowed")
	}
}

// Attack: Yandex returns 200 but with an empty user ID (invalid/revoked token
// that Yandex returns a placeholder response for).
func TestSecurity_YandexEmptyID_FailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(yandexUserInfo{ID: "", RealName: "", DefaultEmail: ""})
	}))
	defer srv.Close()

	a := NewAuth(&mockUserRepo{})
	a.httpClient = testClient(srv.URL)

	_, err := a.ResolveUser(context.Background(), "revoked-token")
	if err == nil {
		t.Error("empty Yandex user ID must be rejected")
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func postWebhook(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/alice/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.webhook(w, r)
	return w
}

func mustDecode(t *testing.T, w *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func assertAccountLinking(t *testing.T, resp aliceResponse) {
	t.Helper()
	if resp.Response.Directives == nil || resp.Response.Directives.StartAccountLinking == nil {
		t.Error("expected start_account_linking directive")
	}
}
