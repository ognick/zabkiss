package alice

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

func TestWebhook_Unauthenticated_ReturnsAccountLinking(t *testing.T) {
	h := &Handler{log: &mockLogger{}, echoSrv: &mockEcho{}, auth: &mockAuth{}}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"","access_token":""}}}`
	r := httptest.NewRequest(http.MethodPost, "/alice/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.webhook(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	var resp aliceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Response.Directives == nil || resp.Response.Directives.StartAccountLinking == nil {
		t.Error("expected start_account_linking directive")
	}
	if resp.Response.Text != errAuth.Error() {
		t.Errorf("Text: got %q, want %q", resp.Response.Text, errAuth.Error())
	}
}

func TestWebhook_Success(t *testing.T) {
	user := &domain.User{ID: "u1", Name: "Иван", Token: "tok"}
	echo := &mockEcho{reply: "включаю свет"}
	h := &Handler{log: &mockLogger{}, echoSrv: echo, auth: &mockAuth{user: user}}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"u1","access_token":"tok"}},"request":{"command":"включи свет"}}`
	r := httptest.NewRequest(http.MethodPost, "/alice/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.webhook(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	var resp aliceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Response.Text != "включаю свет" {
		t.Errorf("Text: got %q, want включаю свет", resp.Response.Text)
	}
	if resp.Response.EndSession {
		t.Error("EndSession should be false on success")
	}
	if resp.Response.Directives != nil {
		t.Error("Directives should be nil on success")
	}
}

func TestWebhook_InvalidBody(t *testing.T) {
	log := &mockLogger{}
	h := &Handler{log: log, echoSrv: &mockEcho{}, auth: &mockAuth{}}

	r := httptest.NewRequest(http.MethodPost, "/alice/webhook", strings.NewReader("{invalid"))
	w := httptest.NewRecorder()

	h.webhook(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	if len(log.logged) == 0 {
		t.Error("expected error to be logged")
	}
	var resp aliceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Response.Text != errParseBody.Error() {
		t.Errorf("Text: got %q, want %q", resp.Response.Text, errParseBody.Error())
	}
	if !resp.Response.EndSession {
		t.Error("EndSession should be true on error")
	}
}

func TestWebhook_AuthError_ReturnsAccountLinking(t *testing.T) {
	log := &mockLogger{}
	h := &Handler{log: log, echoSrv: &mockEcho{}, auth: &mockAuth{err: errors.New("db error")}}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"u1","access_token":"tok"}}}`
	r := httptest.NewRequest(http.MethodPost, "/alice/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.webhook(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	if len(log.logged) == 0 {
		t.Error("expected internal error to be logged")
	}
	var resp aliceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Response.Directives == nil || resp.Response.Directives.StartAccountLinking == nil {
		t.Error("expected start_account_linking on auth error")
	}
}

func TestWebhook_EchoError(t *testing.T) {
	log := &mockLogger{}
	user := &domain.User{ID: "u1", Name: "Иван", Token: "tok"}
	echo := &mockEcho{err: errors.New("llm down")}
	h := &Handler{log: log, echoSrv: echo, auth: &mockAuth{user: user}}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"u1","access_token":"tok"}},"request":{"command":"включи свет"}}`
	r := httptest.NewRequest(http.MethodPost, "/alice/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.webhook(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	var resp aliceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Response.EndSession {
		t.Error("EndSession should be true on echo error")
	}
	if !strings.Contains(resp.Response.Text, "Иван") {
		t.Errorf("Text should contain user name, got: %q", resp.Response.Text)
	}
}

func TestResolveAuth_NoToken_ReturnsErrAuth(t *testing.T) {
	h := &Handler{auth: &mockAuth{}}

	_, err := h.resolveAuth(context.Background(), aliceRequest{})
	if !errors.Is(err, errAuth) {
		t.Errorf("expected errAuth, got: %v", err)
	}
}

func TestResolveAuth_ExistingToken(t *testing.T) {
	user := &domain.User{ID: "u1", Token: "tok"}
	h := &Handler{auth: &mockAuth{user: user}}

	req := aliceRequest{}
	req.Session.User.AccessToken = "tok"
	req.Session.User.UserID = "u1"

	got, err := h.resolveAuth(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "u1" {
		t.Errorf("ID: got %q, want u1", got.ID)
	}
}

func TestWriteError_LogsAndSetsEndSession(t *testing.T) {
	log := &mockLogger{}
	h := &Handler{log: log}
	w := httptest.NewRecorder()

	h.writeError(w, errors.New("что-то пошло не так"))

	if len(log.logged) == 0 {
		t.Error("expected error logged")
	}
	if log.logged[0].msg != "что-то пошло не так" {
		t.Errorf("logged msg: got %q, want %q", log.logged[0].msg, "что-то пошло не так")
	}
	var resp aliceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Response.Text != "что-то пошло не так" {
		t.Errorf("Text: got %q", resp.Response.Text)
	}
	if !resp.Response.EndSession {
		t.Error("EndSession should be true")
	}
}
