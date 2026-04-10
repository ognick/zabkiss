package alice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ognick/zabkiss/internal/domain"
)

// redirectTransport rewrites every request to the given target server (for mocking external APIs).
type redirectTransport struct {
	target string
}

func (rt *redirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	cloned := r.Clone(r.Context())
	parsed, _ := url.Parse(rt.target)
	cloned.URL.Scheme = parsed.Scheme
	cloned.URL.Host = parsed.Host
	return (&http.Transport{}).RoundTrip(cloned)
}

func testClient(serverURL string) *http.Client {
	return &http.Client{Transport: &redirectTransport{target: serverURL}}
}

func TestUserFromContext_Found(t *testing.T) {
	user := domain.User{ID: "abc", Name: "Test"}
	ctx := context.WithValue(context.Background(), contextKey{}, user)
	got, ok := UserFromContext(ctx)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.ID != "abc" {
		t.Errorf("ID: got %q, want abc", got.ID)
	}
}

func TestUserFromContext_NotFound(t *testing.T) {
	_, ok := UserFromContext(context.Background())
	if ok {
		t.Fatal("expected ok=false")
	}
}

func TestAuth_ResolveUser_ExistingToken(t *testing.T) {
	existing := &domain.User{ID: "u1", Token: "tok"}
	a := NewAuth(&mockUserRepo{user: existing})

	user, err := a.ResolveUser(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != "u1" {
		t.Errorf("ID: got %q, want u1", user.ID)
	}
}

func TestAuth_ResolveUser_GetError(t *testing.T) {
	a := NewAuth(&mockUserRepo{getErr: errors.New("db down")})

	_, err := a.ResolveUser(context.Background(), "tok")
	if err == nil {
		t.Error("expected error")
	}
}

func TestAuth_ResolveUser_NewUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(yandexUserInfo{
			ID:           "yid1",
			RealName:     "Иван",
			DefaultEmail: "ivan@ya.ru",
		})
	}))
	defer srv.Close()

	repo := &mockUserRepo{user: nil}
	a := NewAuth(repo)
	a.httpClient = testClient(srv.URL)

	user, err := a.ResolveUser(context.Background(), "mytoken")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != "yid1" {
		t.Errorf("ID: got %q, want yid1", user.ID)
	}
	if user.Token != "mytoken" {
		t.Errorf("Token: got %q, want mytoken", user.Token)
	}
	if repo.upserted == nil {
		t.Error("expected user to be upserted")
	}
}

func TestAuth_ResolveUser_InvalidToken_EmptyID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(yandexUserInfo{})
	}))
	defer srv.Close()

	a := NewAuth(&mockUserRepo{})
	a.httpClient = testClient(srv.URL)

	_, err := a.ResolveUser(context.Background(), "bad")
	if err == nil {
		t.Error("expected error for empty user ID")
	}
}

func TestFetchYandexUserInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(yandexUserInfo{
			ID:           "123",
			RealName:     "Тест",
			DefaultEmail: "test@yandex.ru",
		})
	}))
	defer srv.Close()

	info, err := fetchYandexUserInfo(context.Background(), testClient(srv.URL), "testtoken")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "123" || info.RealName != "Тест" || info.DefaultEmail != "test@yandex.ru" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestFetchYandexUserInfo_SendsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(yandexUserInfo{ID: "x"})
	}))
	defer srv.Close()

	fetchYandexUserInfo(context.Background(), testClient(srv.URL), "mytoken")

	if gotAuth != "Bearer mytoken" {
		t.Errorf("Authorization: got %q, want Bearer mytoken", gotAuth)
	}
}
