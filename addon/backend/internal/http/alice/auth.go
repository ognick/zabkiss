package alice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ognick/zabkiss/internal/domain"
	"github.com/ognick/zabkiss/internal/repository"
)

var errForbidden = errors.New("у вас нет доступа")

// YandexAuth resolves a Yandex OAuth token to a domain.User,
// caching the result in the local DB.
type YandexAuth struct {
	userRepo      repository.UserRepo
	httpClient    *http.Client
	allowedEmails []string
}

func NewAuth(userRepo repository.UserRepo, allowedEmails []string) *YandexAuth {
	return &YandexAuth{
		userRepo:      userRepo,
		httpClient:    http.DefaultClient,
		allowedEmails: allowedEmails,
	}
}

// WithHTTPClient replaces the HTTP client used for Yandex API calls.
// Intended for testing: points requests at a mock server instead of login.yandex.ru.
func (a *YandexAuth) WithHTTPClient(client *http.Client) *YandexAuth {
	a.httpClient = client
	return a
}

func (a *YandexAuth) ResolveUser(ctx context.Context, token string) (domain.User, error) {
	existing, err := a.userRepo.GetByToken(ctx, token)
	if err != nil {
		return domain.User{}, err
	}
	if existing != nil {
		return a.checkAllowed(*existing)
	}

	info, err := fetchYandexUserInfo(ctx, a.httpClient, token)
	if err != nil {
		return domain.User{}, fmt.Errorf("yandex user info: %w", err)
	}
	if info.ID == "" {
		return domain.User{}, fmt.Errorf("invalid token")
	}

	user := domain.User{
		ID:    info.ID,
		Name:  info.RealName,
		Email: info.DefaultEmail,
		Token: token,
	}
	if err := a.userRepo.Upsert(ctx, user); err != nil {
		return domain.User{}, err
	}
	return a.checkAllowed(user)
}

func (a *YandexAuth) checkAllowed(user domain.User) (domain.User, error) {
	for _, e := range a.allowedEmails {
		if e == user.Email {
			return user, nil
		}
	}
	return user, errForbidden
}

type yandexUserInfo struct {
	ID           string `json:"id"`
	RealName     string `json:"real_name"`
	DefaultEmail string `json:"default_email"`
}

func fetchYandexUserInfo(ctx context.Context, client *http.Client, token string) (yandexUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://login.yandex.ru/info", nil)
	if err != nil {
		return yandexUserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return yandexUserInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return yandexUserInfo{}, fmt.Errorf("yandex API: unexpected status %d", resp.StatusCode)
	}

	var info yandexUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return yandexUserInfo{}, err
	}
	return info, nil
}
