package alice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ognick/zabkiss/internal/domain"
	"github.com/ognick/zabkiss/internal/repository"
)

type contextKey struct{}

func UserFromContext(ctx context.Context) (domain.User, bool) {
	u, ok := ctx.Value(contextKey{}).(domain.User)
	return u, ok
}

type YandexAuth struct {
	userRepo   repository.UserRepo
	httpClient *http.Client
}

func NewAuth(userRepo repository.UserRepo) *YandexAuth {
	return &YandexAuth{userRepo: userRepo, httpClient: http.DefaultClient}
}

func (a *YandexAuth) ResolveUser(ctx context.Context, token string) (domain.User, error) {
	existing, err := a.userRepo.GetByToken(ctx, token)
	if err != nil {
		return domain.User{}, err
	}
	if existing != nil {
		return *existing, nil
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
	return user, nil
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
