package ha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ognick/zabkiss/internal/domain"
)

const svcCacheTTL = time.Hour

// Client взаимодействует с Home Assistant REST API.
type Client interface {
	// GetDeviceInfos возвращает состояние + применимые сервисы для каждого entity.
	GetDeviceInfos(ctx context.Context, entityIDs []string) ([]domain.Device, error)
	// CallService вызывает сервис HA для конкретного entity.
	CallService(ctx context.Context, entityID, service string, params map[string]any) error
}

type haClient struct {
	haURL string
	token string

	svcMu      sync.Mutex
	svcCache   map[string][]applicableService // domain → services
	svcFetched time.Time
}

func NewClient(haURL, token string) Client {
	return &haClient{haURL: haURL, token: token}
}

// GetDeviceInfos фетчит /api/states и /api/services, строит domain.Device для каждого entity.
func (c *haClient) GetDeviceInfos(ctx context.Context, entityIDs []string) ([]domain.Device, error) {
	states, err := c.fetchStates(ctx, entityIDs)
	if err != nil {
		return nil, err
	}

	allSvcs, _ := c.cachedServices(ctx) // ошибка не фатальна — Services будет nil

	devices := make([]domain.Device, 0, len(states))
	for _, s := range states {
		devices = append(devices, toDevice(s, allSvcs[s.haDomain()]))
	}
	return devices, nil
}

// CallService вызывает POST /api/services/{domain}/{service}.
func (c *haClient) CallService(ctx context.Context, entityID, service string, params map[string]any) error {
	parts := strings.SplitN(service, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid service %q: expected domain.service", service)
	}
	domain, svc := parts[0], parts[1]

	body := make(map[string]any, len(params)+1)
	for k, v := range params {
		body[k] = v
	}
	body["entity_id"] = entityID

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}

	url := fmt.Sprintf("%s/api/services/%s/%s", c.haURL, domain, svc)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("call service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("service %s returned %d", service, resp.StatusCode)
	}
	return nil
}

// ── services cache ────────────────────────────────────────────────────────────

func (c *haClient) cachedServices(ctx context.Context) (map[string][]applicableService, error) {
	c.svcMu.Lock()
	defer c.svcMu.Unlock()

	if c.svcCache != nil && time.Since(c.svcFetched) < svcCacheTTL {
		return c.svcCache, nil
	}

	cache, err := c.fetchAndParseServices(ctx)
	if err != nil {
		return c.svcCache, err // при ошибке отдаём устаревший кеш
	}
	c.svcCache = cache
	c.svcFetched = time.Now()
	return cache, nil
}

func (c *haClient) fetchAndParseServices(ctx context.Context) (map[string][]applicableService, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.haURL+"/api/services", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch services: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("services api returned %d", resp.StatusCode)
	}

	var raw []struct {
		Domain   string `json:"domain"`
		Services map[string]struct {
			Fields map[string]struct {
				Selector map[string]json.RawMessage `json:"selector"`
				Advanced bool                       `json:"advanced"`
			} `json:"fields"`
		} `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode services: %w", err)
	}

	result := make(map[string][]applicableService, len(raw))
	for _, domainEntry := range raw {
		var svcs []applicableService
		for svcName, svcDef := range domainEntry.Services {
			applicable := applicableService{
				Service: domainEntry.Domain + "." + svcName,
				Params:  make(map[string]param),
			}
			for fieldName, field := range svcDef.Fields {
				if field.Advanced {
					continue
				}
				if p, ok := parseSelector(field.Selector); ok {
					applicable.Params[fieldName] = p
				}
			}
			svcs = append(svcs, applicable)
		}
		result[domainEntry.Domain] = svcs
	}
	return result, nil
}

// parseSelector конвертирует HA selector в param.
// Поддерживаем number, select, boolean, color_rgb — остальные пропускаем.
func parseSelector(sel map[string]json.RawMessage) (param, bool) {
	if raw, ok := sel["number"]; ok {
		var s struct {
			Min  *float64        `json:"min"`
			Max  *float64        `json:"max"`
			Step json.RawMessage `json:"step"` // can be "any" (string) or a number — ignore value
		}
		if err := json.Unmarshal(raw, &s); err == nil {
			p := param{Type: domain.ParamTypeNumber}
			if s.Min != nil {
				p.Min = *s.Min
			}
			if s.Max != nil {
				p.Max = *s.Max
			}
			return p, true
		}
	}

	if raw, ok := sel["select"]; ok {
		var s struct {
			Options json.RawMessage `json:"options"`
		}
		if err := json.Unmarshal(raw, &s); err == nil {
			values := parseSelectOptions(s.Options)
			if len(values) > 0 {
				return param{Type: domain.ParamTypeSelect, Values: values}, true
			}
		}
	}

	if _, ok := sel["boolean"]; ok {
		return param{Type: domain.ParamTypeBoolean}, true
	}

	if _, ok := sel["color_rgb"]; ok {
		return param{Type: domain.ParamTypeRGB}, true
	}

	return param{}, false
}

// parseSelectOptions обрабатывает оба формата options из HA:
// ["opt1", "opt2"] или [{"value": "opt1", "label": "Option 1"}, ...]
func parseSelectOptions(raw json.RawMessage) []string {
	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		return asStrings
	}

	var asObjects []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal(raw, &asObjects); err == nil {
		result := make([]string, 0, len(asObjects))
		for _, o := range asObjects {
			result = append(result, o.Value)
		}
		return result
	}
	return nil
}

// ── states ────────────────────────────────────────────────────────────────────

func (c *haClient) fetchStates(ctx context.Context, entityIDs []string) ([]entityState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.haURL+"/api/states", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch states: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("states api returned %d", resp.StatusCode)
	}

	var raw []struct {
		EntityID   string         `json:"entity_id"`
		State      string         `json:"state"`
		Attributes map[string]any `json:"attributes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode states: %w", err)
	}

	wanted := make(map[string]bool, len(entityIDs))
	for _, id := range entityIDs {
		wanted[id] = true
	}

	result := make([]entityState, 0, len(entityIDs))
	for _, item := range raw {
		if !wanted[item.EntityID] {
			continue
		}
		name, _ := item.Attributes["friendly_name"].(string)
		if name == "" {
			name = item.EntityID
		}
		result = append(result, entityState{
			EntityID:     item.EntityID,
			State:        item.State,
			FriendlyName: name,
			Attributes:   item.Attributes,
		})
	}
	return result, nil
}
