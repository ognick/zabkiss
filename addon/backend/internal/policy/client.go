package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ognick/zabkiss/pkg/logger"
)

type Client interface {
	GetEntities(ctx context.Context) ([]string, error)
}

type HAClient struct {
	haURL string
	token string
	ttl   time.Duration
	log   logger.Logger

	mu        sync.Mutex
	cached    []string
	fetchedAt time.Time
}

func NewClient(haURL, token string, ttl time.Duration, log logger.Logger) *HAClient {
	return &HAClient{haURL: haURL, token: token, ttl: ttl, log: log}
}

// Run реализует lifecycle компонент: прогревает кеш при старте, затем рефрешит в фоне.
func (c *HAClient) Run(ctx context.Context, probe func(error)) error {
	if err := c.refresh(ctx); err != nil {
		c.log.Error("initial policy fetch failed", "err", err)
	}
	probe(nil)

	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.refresh(ctx); err != nil {
				c.log.Error("policy refresh failed", "err", err)
			}
		}
	}
}

func (c *HAClient) GetEntities(ctx context.Context) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < c.ttl {
		return c.cached, nil
	}

	// Фоновый рефреш не успел — фетчим синхронно.
	return c.fetchLocked(ctx)
}

func (c *HAClient) refresh(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.fetchLocked(ctx)
	return err
}

func (c *HAClient) fetchLocked(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.haURL+"/api/zabkiss/policy", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch policy: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("policy api returned %d", resp.StatusCode)
	}

	var body struct {
		Entities []string `json:"entities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode policy: %w", err)
	}

	c.cached = body.Entities
	c.fetchedAt = time.Now()
	return c.cached, nil
}
