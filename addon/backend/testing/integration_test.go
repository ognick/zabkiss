//go:build integration

package e2e

// Acceptance test: spins up a real Home Assistant container with the zabkiss
// custom integration installed, then exercises the full Alice→Go server→HA flow.
//
// Run with:
//
//	go test -v -tags integration -timeout 10m ./testing/

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ognick/zabkiss/internal/http/alice"
	"github.com/ognick/zabkiss/internal/policy"
	sqliterepo "github.com/ognick/zabkiss/internal/repository/sqlite"
	"github.com/ognick/zabkiss/pkg/httpserver"
)

const haImage = "homeassistant/home-assistant:stable"

// TestAcceptance_AliceFullFlow is the top-level acceptance test.
// It starts HA, installs the integration, seeds policy data, then verifies
// the Alice webhook flow end-to-end using a real policy.HAClient.
func TestAcceptance_AliceFullFlow(t *testing.T) {
	ctx := context.Background()

	// ── Phase 1: infrastructure ───────────────────────────────────────────────
	configDir := prepareHAConfig(t)
	haURL := startHA(t, ctx, configDir)
	t.Logf("HA running at %s", haURL)

	token := onboardHA(t, haURL)
	t.Logf("HA admin token obtained")

	setupZabkissIntegration(t, haURL, token)
	t.Logf("zabkiss integration configured")

	entities := []string{"light.living_room", "switch.kitchen"}
	seedPolicy(t, haURL, token, entities)
	t.Logf("policy seeded: %v", entities)

	// ── Phase 2: verify HA policy endpoint directly ───────────────────────────
	t.Run("HA policy endpoint returns entities", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, haURL+"/api/zabkiss/policy", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /api/zabkiss/policy: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status %d: %s", resp.StatusCode, body)
		}
		var pol struct {
			Entities []string `json:"entities"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&pol); err != nil {
			t.Fatal(err)
		}
		if len(pol.Entities) == 0 {
			t.Error("expected non-empty entities list from HA")
		}
		t.Logf("entities from HA: %v", pol.Entities)
	})

	// ── Phase 3: full Alice→Go server→HA flow ────────────────────────────────
	srv := newIntegrationServer(t, haURL, token, []string{"alice@home.ru"})
	srv.Yandex.register("acc-tok", yandexUser{
		ID: "acc-id", Name: "Алиса", Email: "alice@home.ru",
	})

	t.Run("allowed user executes command", func(t *testing.T) {
		r := decodeResp(t, srv.post(t, authedReq("acc-id", "acc-tok", "включи свет")))
		if r.Response.Text == "" {
			t.Fatal("expected non-empty response text")
		}
		if r.Response.Directives != nil && r.Response.Directives.StartAccountLinking != nil {
			t.Fatal("unexpected account-linking — authentication failed")
		}
		t.Logf("response: %q", r.Response.Text)
	})

	t.Run("unauthenticated user gets account-linking", func(t *testing.T) {
		r := decodeResp(t, srv.post(t, unauthReq("включи свет")))
		assertAccountLinking(t, r)
	})

	t.Run("blocked email gets personal denial", func(t *testing.T) {
		blockedSrv := newIntegrationServer(t, haURL, token, []string{"owner@home.ru"})
		blockedSrv.Yandex.register("att-tok", yandexUser{
			ID: "att-id", Name: "Злоумышленник", Email: "attacker@evil.com",
		})
		r := decodeResp(t, blockedSrv.post(t, authedReq("att-id", "att-tok", "открой замок")))
		if r.Response.Directives != nil && r.Response.Directives.StartAccountLinking != nil {
			t.Error("authenticated-but-blocked user must not get account-linking")
		}
		if !strings.Contains(r.Response.Text, "Злоумышленник") {
			t.Errorf("denial must contain user name, got: %q", r.Response.Text)
		}
	})

	t.Run("ping bypasses auth", func(t *testing.T) {
		r := decodeResp(t, srv.post(t, pingReq()))
		if r.Response.Text != "ok" {
			t.Errorf("ping: got %q, want ok", r.Response.Text)
		}
	})
}

// ── HA helpers ────────────────────────────────────────────────────────────────

// prepareHAConfig creates a temp directory with:
//   - configuration.yaml (minimal, no default_config for faster startup)
//   - custom_components/zabkiss/ (copied from the repo)
func prepareHAConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	// Minimal config.yaml — prevents HA from auto-loading default_config
	configYAML := "homeassistant:\n  name: ZabKissTest\n"
	if err := os.WriteFile(filepath.Join(dir, "configuration.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Copy integration files into the config dir
	src, err := filepath.Abs(filepath.Join("..", "..", "integration", "custom_components", "zabkiss"))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "custom_components", "zabkiss")
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copy integration: %v", err)
	}

	return dir
}

// startHA starts a Home Assistant container, mounts configDir as /config,
// and waits until the HA REST API is ready (returns 401 = running but needs auth).
func startHA(t *testing.T, ctx context.Context, configDir string) string {
	t.Helper()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        haImage,
			ExposedPorts: []string{"8123/tcp"},
			Mounts: testcontainers.Mounts(
				testcontainers.BindMount(configDir, "/config"),
			),
			WaitingFor: wait.ForHTTP("/api/").
				WithPort("8123").
				WithStatusCodeMatcher(func(status int) bool {
					// 401 = HA is up and requires auth (expected)
					// 200 = HA responded (shouldn't normally happen unauthenticated, but accept it)
					return status == http.StatusUnauthorized || status == http.StatusOK
				}).
				WithStartupTimeout(3 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start HA container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("terminate HA container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "8123")
	if err != nil {
		t.Fatal(err)
	}

	return fmt.Sprintf("http://%s:%s", host, port.Port())
}

// onboardHA creates the first admin user via HA's onboarding API and returns
// an access token. Must be called before any authenticated API call.
func onboardHA(t *testing.T, haURL string) string {
	t.Helper()

	client := &http.Client{Timeout: 30 * time.Second}
	clientID := haURL + "/"

	// Step 1: create admin user
	payload, _ := json.Marshal(map[string]string{
		"client_id": clientID,
		"name":      "Test Admin",
		"username":  "testadmin",
		"password":  "testpass1234",
		"language":  "en",
	})
	resp, err := client.Post(haURL+"/api/onboarding/users", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("onboarding/users: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("onboarding/users: status %d: %s", resp.StatusCode, body)
	}

	var onboardResult struct {
		AuthCode string `json:"auth_code"`
	}
	if err := json.Unmarshal(body, &onboardResult); err != nil {
		t.Fatalf("decode onboarding response: %v (body: %s)", err, body)
	}
	if onboardResult.AuthCode == "" {
		t.Fatalf("empty auth_code from onboarding (body: %s)", body)
	}

	// Step 2: exchange auth_code for access token
	data := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {onboardResult.AuthCode},
		"client_id":  {clientID},
	}
	resp2, err := client.Post(haURL+"/auth/token", "application/x-www-form-urlencoded",
		strings.NewReader(data.Encode()))
	if err != nil {
		t.Fatalf("auth/token: %v", err)
	}
	defer resp2.Body.Close()

	body2, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("auth/token: status %d: %s", resp2.StatusCode, body2)
	}

	var tokenResult struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body2, &tokenResult); err != nil {
		t.Fatalf("decode token response: %v (body: %s)", err, body2)
	}
	if tokenResult.AccessToken == "" {
		t.Fatalf("empty access_token (body: %s)", body2)
	}

	return tokenResult.AccessToken
}

// setupZabkissIntegration adds the zabkiss integration via the HA config-entries
// flow API. Since the config flow requires no user input, we start it and
// optionally submit an empty form to advance past any intermediate step.
func setupZabkissIntegration(t *testing.T, haURL, token string) {
	t.Helper()

	client := &http.Client{Timeout: 30 * time.Second}
	authHdr := "Bearer " + token

	// Start config flow for the zabkiss handler
	payload, _ := json.Marshal(map[string]string{"handler": "zabkiss"})
	req, _ := http.NewRequest(http.MethodPost,
		haURL+"/api/config/config_entries/flow", bytes.NewReader(payload))
	req.Header.Set("Authorization", authHdr)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("start config flow: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("config flow started: %s", body)

	var flow struct {
		FlowID string `json:"flow_id"`
		Type   string `json:"type"` // "form" → needs submit; "create_entry" → done
	}
	if err := json.Unmarshal(body, &flow); err != nil {
		t.Fatalf("decode flow response: %v", err)
	}
	if resp.StatusCode >= 400 {
		t.Fatalf("config flow returned %d: %s", resp.StatusCode, body)
	}

	// If HA returned a form step, submit empty data to advance to create_entry
	if flow.Type == "form" && flow.FlowID != "" {
		req2, _ := http.NewRequest(http.MethodPost,
			haURL+"/api/config/config_entries/flow/"+flow.FlowID,
			bytes.NewReader([]byte("{}")))
		req2.Header.Set("Authorization", authHdr)
		req2.Header.Set("Content-Type", "application/json")

		resp2, err := client.Do(req2)
		if err != nil {
			t.Fatalf("submit config flow: %v", err)
		}
		b2, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		t.Logf("config flow result: %s", b2)
	}
}

// seedPolicy POSTs a list of entity IDs to the zabkiss policy endpoint.
func seedPolicy(t *testing.T, haURL, token string, entities []string) {
	t.Helper()

	client := &http.Client{Timeout: 30 * time.Second}

	payload, _ := json.Marshal(map[string][]string{"entities": entities})
	req, _ := http.NewRequest(http.MethodPost, haURL+"/api/zabkiss/policy",
		bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed policy: status %d: %s", resp.StatusCode, body)
	}
}

// newIntegrationServer creates a test server that uses a real policy.HAClient
// pointing at the provided HA instance. Yandex OAuth is still mocked.
func newIntegrationServer(t *testing.T, haURL, haToken string, allowedEmails []string) *testServer {
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
	policyClient := policy.NewClient(haURL, haToken, 30*time.Second, log)

	r := chi.NewRouter()
	r.Use(httpserver.RecoveryMiddleware(log))
	alice.New(
		&echoStub{reply: "включаю свет"},
		auth,
		policyClient,
		log,
		allowedEmails,
	).Register(r)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &testServer{URL: srv.URL, Yandex: yandex}
}

// ── File utils ────────────────────────────────────────────────────────────────

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
