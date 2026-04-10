package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"

	"github.com/ognick/zabkiss/internal/http/alice"
	sqliterepo "github.com/ognick/zabkiss/internal/repository/sqlite"
	"github.com/ognick/zabkiss/pkg/httpserver"
	"github.com/ognick/zabkiss/pkg/logger"
)

// ── Yandex OAuth mock ─────────────────────────────────────────────────────────

// yandexUser is the user info that the mock Yandex API will return for a given token.
type yandexUser struct {
	ID    string
	Name  string
	Email string
}

// yandexMock is a fake Yandex OAuth server. It returns the registered user for
// known tokens and 401 for everything else.
type yandexMock struct {
	mu    sync.RWMutex
	users map[string]yandexUser
	Srv   *httptest.Server
}

func newYandexMock(t *testing.T) *yandexMock {
	t.Helper()
	m := &yandexMock{users: make(map[string]yandexUser)}
	m.Srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		m.mu.RLock()
		u, ok := m.users[token]
		m.mu.RUnlock()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"message": "OAuth token is invalid"}) //nolint:errcheck
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"id":            u.ID,
			"real_name":     u.Name,
			"default_email": u.Email,
		})
	}))
	t.Cleanup(m.Srv.Close)
	return m
}

// register makes the mock accept the given token and return the given user.
func (m *yandexMock) register(token string, u yandexUser) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[token] = u
}

// client returns an *http.Client that routes all requests to the mock server.
func (m *yandexMock) client() *http.Client {
	return &http.Client{Transport: &singleHostTransport{
		target: m.Srv.URL,
		base:   &http.Transport{},
	}}
}

// singleHostTransport redirects every outgoing HTTP request to a fixed host,
// preserving the path and headers. Used to redirect Yandex API calls to a mock.
type singleHostTransport struct {
	target string
	base   *http.Transport
}

func (t *singleHostTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	cloned := r.Clone(r.Context())
	parsed, _ := url.Parse(t.target)
	cloned.URL.Scheme = parsed.Scheme
	cloned.URL.Host = parsed.Host
	return t.base.RoundTrip(cloned)
}

// ── Echo stub ─────────────────────────────────────────────────────────────────

type echoStub struct {
	reply string
	err   error
}

func (e *echoStub) Say(_ context.Context, _ string, _ []string) (string, error) {
	return e.reply, e.err
}

// panicEchoStub simulates a catastrophic failure in the echo service.
type panicEchoStub struct{}

func (e *panicEchoStub) Say(_ context.Context, _ string, _ []string) (string, error) {
	panic("simulated LLM panic")
}

// policyStub returns a fixed list of entities (empty by default).
type policyStub struct {
	entities []string
}

func (p *policyStub) GetEntities(_ context.Context) ([]string, error) {
	return p.entities, nil
}

// ── Test server ───────────────────────────────────────────────────────────────

// testServer is a fully wired-up instance of the service running on a
// random port. All external dependencies are replaced with in-process mocks.
type testServer struct {
	URL    string
	Yandex *yandexMock
	echo   *echoStub
}

type serverConfig struct {
	allowedEmails []string
	echoReply     string
	echoErr       error
}

// newServer starts a test server and registers cleanup with t.Cleanup.
// Components: real chi router + RecoveryMiddleware + alice.Handler +
// real YandexAuth pointing at the mock + in-memory SQLite.
func newServer(t *testing.T, cfg serverConfig) *testServer {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	userRepo, err := sqliterepo.NewUserRepo(db)
	if err != nil {
		t.Fatal(err)
	}

	yandex := newYandexMock(t)
	auth := alice.NewAuth(userRepo).WithHTTPClient(yandex.client())

	reply := cfg.echoReply
	if reply == "" {
		reply = "ответ системы"
	}
	echo := &echoStub{reply: reply, err: cfg.echoErr}

	log := newTestLogger(t)

	r := chi.NewRouter()
	r.Use(httpserver.RecoveryMiddleware(log))
	alice.New(echo, auth, &policyStub{}, log, cfg.allowedEmails).Register(r)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &testServer{URL: srv.URL, Yandex: yandex, echo: echo}
}

// newServerWithCustomEcho is used when tests need non-standard echo behaviour
// (e.g., panic injection). Accepts any value with a Say method.
func newServerWithCustomEcho(t *testing.T, echo interface {
	Say(context.Context, string, []string) (string, error)
}, cfg serverConfig) *testServer {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	userRepo, err := sqliterepo.NewUserRepo(db)
	if err != nil {
		t.Fatal(err)
	}

	yandex := newYandexMock(t)
	auth := alice.NewAuth(userRepo).WithHTTPClient(yandex.client())
	log := newTestLogger(t)

	r := chi.NewRouter()
	r.Use(httpserver.RecoveryMiddleware(log))
	alice.New(echo, auth, &policyStub{}, log, cfg.allowedEmails).Register(r)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &testServer{URL: srv.URL, Yandex: yandex}
}

// ── Logger ────────────────────────────────────────────────────────────────────

type testLogger struct{ t *testing.T }

func newTestLogger(t *testing.T) logger.Logger { return &testLogger{t: t} }

func (l *testLogger) Info(msg string, args ...any)  { l.t.Logf("INFO  %s %v", msg, args) }
func (l *testLogger) Error(msg string, args ...any) { l.t.Logf("ERROR %s %v", msg, args) }
func (l *testLogger) Debug(msg string, args ...any) {}
func (l *testLogger) Warn(msg string, args ...any)  { l.t.Logf("WARN  %s %v", msg, args) }

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func (s *testServer) post(t *testing.T, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(s.URL+"/alice/webhook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /alice/webhook: %v", err)
	}
	return resp
}

func (s *testServer) do(t *testing.T, method, path string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, s.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func decodeResp(t *testing.T, resp *http.Response) webhookResponse {
	t.Helper()
	defer resp.Body.Close()
	var r webhookResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode response (status %d): %v", resp.StatusCode, err)
	}
	return r
}

// ── Response types (mirror alice package's unexported types) ──────────────────

type webhookResponse struct {
	Version  string       `json:"version"`
	Response responseBody `json:"response"`
}

type responseBody struct {
	Text       string      `json:"text"`
	TTS        string      `json:"tts,omitempty"`
	EndSession bool        `json:"end_session"`
	Directives *directives `json:"directives,omitempty"`
}

type directives struct {
	StartAccountLinking *struct{} `json:"start_account_linking,omitempty"`
}

// ── Request builders ──────────────────────────────────────────────────────────

// authedReq builds an Alice webhook JSON body for an authenticated user.
func authedReq(userID, token, command string) string {
	return fmt.Sprintf(
		`{"session":{"session_id":"s1","message_id":1,"user":{"user_id":%q,"access_token":%q}},"request":{"command":%q,"original_utterance":%q}}`,
		userID, token, command, command,
	)
}

// unauthReq builds a request with no credentials.
func unauthReq(command string) string {
	return fmt.Sprintf(
		`{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"","access_token":""}},"request":{"command":%q,"original_utterance":%q}}`,
		command, command,
	)
}

// pingReq builds an Alice health-check request (bypasses auth).
func pingReq() string {
	return `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"","access_token":""}},"request":{"command":"","original_utterance":"ping"}}`
}

// ── Assertion helpers ─────────────────────────────────────────────────────────

func assertAccountLinking(t *testing.T, r webhookResponse) {
	t.Helper()
	if r.Response.Directives == nil || r.Response.Directives.StartAccountLinking == nil {
		t.Errorf("expected start_account_linking directive, response text: %q", r.Response.Text)
	}
}
