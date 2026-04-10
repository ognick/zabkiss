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
	h := &Handler{log: &mockLogger{}, svc: &mockService{}, auth: &mockAuth{}}

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

func TestWebhook_Success_EndsSession(t *testing.T) {
	user := &domain.User{ID: "u1", Name: "Иван", Email: "ivan@home.ru", Token: "tok"}
	svc := &mockService{result: domain.CommandResult{Status: domain.CommandOK, Reply: "включаю свет"}}
	h := &Handler{log: &mockLogger{}, svc: svc, auth: &mockAuth{user: user}}

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
	if !resp.Response.EndSession {
		t.Error("EndSession should be true for status ok")
	}
	if resp.Response.Directives != nil {
		t.Error("Directives should be nil on success")
	}
}

func TestWebhook_Clarify_KeepsSession(t *testing.T) {
	user := &domain.User{ID: "u1", Name: "Иван", Email: "ivan@home.ru", Token: "tok"}
	svc := &mockService{result: domain.CommandResult{Status: domain.CommandClarify, Reply: "какую именно лампочку включить?"}}
	h := &Handler{log: &mockLogger{}, svc: svc, auth: &mockAuth{user: user}}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"u1","access_token":"tok"}},"request":{"command":"включи свет"}}`
	r := httptest.NewRequest(http.MethodPost, "/alice/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.webhook(w, r)

	var resp aliceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Response.EndSession {
		t.Error("EndSession should be false for status clarify")
	}
	if resp.Response.Text != "какую именно лампочку включить?" {
		t.Errorf("Text: got %q", resp.Response.Text)
	}
}

func TestWebhook_InvalidBody(t *testing.T) {
	log := &mockLogger{}
	h := &Handler{log: log, svc: &mockService{}, auth: &mockAuth{}}

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
	h := &Handler{log: log, svc: &mockService{}, auth: &mockAuth{err: errors.New("db error")}}

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

func TestWebhook_Forbidden_ReturnsDenialWithName(t *testing.T) {
	log := &mockLogger{}
	user := &domain.User{ID: "u1", Name: "Злоумышленник", Email: "bad@evil.com"}
	h := &Handler{log: log, svc: &mockService{}, auth: &mockAuth{user: user, err: errForbidden}}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"u1","access_token":"tok"}}}`
	r := httptest.NewRequest(http.MethodPost, "/alice/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.webhook(w, r)

	var resp aliceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Response.Text, "Злоумышленник") {
		t.Errorf("denial should include user name, got: %q", resp.Response.Text)
	}
	if resp.Response.Directives != nil && resp.Response.Directives.StartAccountLinking != nil {
		t.Error("forbidden should not trigger account linking")
	}
}

func TestWebhook_ServiceError(t *testing.T) {
	log := &mockLogger{}
	user := &domain.User{ID: "u1", Name: "Иван", Email: "ivan@home.ru", Token: "tok"}
	h := &Handler{log: log, svc: &mockService{err: errors.New("llm down")}, auth: &mockAuth{user: user}}

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
		t.Error("EndSession should be true on service error")
	}
	if !strings.Contains(resp.Response.Text, "Иван") {
		t.Errorf("Text should contain user name, got: %q", resp.Response.Text)
	}
}

func TestWebhook_Timeout_ReturnsRetryMessage(t *testing.T) {
	log := &mockLogger{}
	user := &domain.User{ID: "u1", Name: "Иван", Email: "ivan@home.ru", Token: "tok"}
	h := &Handler{log: log, svc: &mockService{err: context.DeadlineExceeded}, auth: &mockAuth{user: user}}

	body := `{"session":{"session_id":"s1","message_id":1,"user":{"user_id":"u1","access_token":"tok"}},"request":{"command":"включи свет"}}`
	r := httptest.NewRequest(http.MethodPost, "/alice/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.webhook(w, r)

	var resp aliceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Response.EndSession {
		t.Error("EndSession should be true on timeout")
	}
	if !strings.Contains(resp.Response.Text, "попробуйте ещё раз") {
		t.Errorf("timeout message should suggest retry, got: %q", resp.Response.Text)
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
