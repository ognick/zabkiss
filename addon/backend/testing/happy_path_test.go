package e2e

import (
	"net/http"
	"testing"
)

// TestE2E_Ping verifies the built-in health-check utterance.
// It bypasses auth intentionally and must always respond "ok".
func TestE2E_Ping(t *testing.T) {
	srv := newServer(t, serverConfig{})

	resp := srv.post(t, pingReq())
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	r := decodeResp(t, resp)
	if r.Response.Text != "ok" {
		t.Errorf("ping response text: got %q, want ok", r.Response.Text)
	}
	if r.Response.EndSession {
		t.Error("ping must not end the session")
	}
}

// TestE2E_ExistingUser_CommandExecuted covers the hot path:
// token already in DB → skips Yandex API call → executes command.
func TestE2E_ExistingUser_CommandExecuted(t *testing.T) {
	const (
		token = "tok-alice"
		want  = "включаю свет"
	)
	srv := newServer(t, serverConfig{
		allowedEmails: []string{"alice@home.ru"},
		echoReply:     want,
	})
	srv.Yandex.register(token, yandexUser{ID: "yid1", Name: "Алиса", Email: "alice@home.ru"})

	// First request: user unknown to DB, Yandex resolves and saves her.
	r1 := decodeResp(t, srv.post(t, authedReq("yid1", token, "включи свет")))
	if r1.Response.Text != want {
		t.Fatalf("1st request: got %q, want %q", r1.Response.Text, want)
	}
	if !r1.Response.EndSession {
		t.Error("success (status ok) must end the session")
	}

	// Second request: user now cached in DB. Yandex mock could be shut down
	// and this would still work.
	r2 := decodeResp(t, srv.post(t, authedReq("yid1", token, "включи свет")))
	if r2.Response.Text != want {
		t.Errorf("2nd request (from DB cache): got %q, want %q", r2.Response.Text, want)
	}
}

// TestE2E_NewUser_AutoRegistered covers user auto-registration:
// token not in DB → Yandex confirms → user is saved → command runs.
func TestE2E_NewUser_AutoRegistered(t *testing.T) {
	srv := newServer(t, serverConfig{
		allowedEmails: []string{"new@home.ru"},
		echoReply:     "готово",
	})
	srv.Yandex.register("new-tok", yandexUser{ID: "new-id", Name: "Новый", Email: "new@home.ru"})

	r := decodeResp(t, srv.post(t, authedReq("new-id", "new-tok", "включи кофемашину")))
	if r.Response.Text != "готово" {
		t.Errorf("got %q, want готово", r.Response.Text)
	}
	if r.Response.Directives != nil {
		t.Error("new registered user must not get account-linking directive")
	}
}

// TestE2E_AllowedEmail_HasAccess verifies that a user whose email appears
// in the allowlist can execute commands end-to-end.
func TestE2E_AllowedEmail_HasAccess(t *testing.T) {
	srv := newServer(t, serverConfig{
		allowedEmails: []string{"owner@home.ru"},
		echoReply:     "выполняю",
	})
	srv.Yandex.register("owner-tok", yandexUser{ID: "owner-id", Name: "Владелец", Email: "owner@home.ru"})

	r := decodeResp(t, srv.post(t, authedReq("owner-id", "owner-tok", "выключи всё")))
	if r.Response.Text != "выполняю" {
		t.Errorf("got %q, want выполняю", r.Response.Text)
	}
}

// TestE2E_HealthEndpoint verifies the /health HTTP endpoint is reachable.
func TestE2E_HealthEndpoint(t *testing.T) {
	srv := newServer(t, serverConfig{})

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health: got %d, want 200", resp.StatusCode)
	}
}
