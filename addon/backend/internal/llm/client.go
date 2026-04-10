package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ognick/zabkiss/internal/domain"
)

// Client отправляет запрос в LLM и возвращает структурированный ответ.
type Client interface {
	Execute(ctx context.Context, command string, devices []domain.Device, history []domain.ChatMessage) (domain.CommandResult, error)
}

type openAIClient struct {
	baseURL string
	apiKey  string
	model   string
}

func NewClient(baseURL, apiKey, model string) Client {
	return &openAIClient{baseURL: baseURL, apiKey: apiKey, model: model}
}

type chatRequest struct {
	Model          string    `json:"model"`
	Messages       []message `json:"messages"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// llmResponse — внутренний DTO для парсинга ответа LLM.
type llmResponse struct {
	Status  string          `json:"status"`
	Reply   string          `json:"reply"`
	Reason  string          `json:"reason"`
	Actions []llmAction     `json:"actions"`
}

type llmAction struct {
	TargetID string         `json:"target_id"`
	Service  string         `json:"service"`
	Data     map[string]any `json:"data"`
}

func (c *openAIClient) Execute(ctx context.Context, command string, devices []domain.Device, history []domain.ChatMessage) (domain.CommandResult, error) {
	systemPrompt := BuildSystemPrompt(devices)

	messages := make([]message, 0, len(history)+2)
	messages = append(messages, message{Role: "system", Content: systemPrompt})
	for _, h := range history {
		messages = append(messages, message{Role: h.Role, Content: h.Content})
	}
	messages = append(messages, message{Role: "user", Content: command})

	reqBody := chatRequest{
		Model:    c.model,
		Messages: messages,
	}
	reqBody.ResponseFormat.Type = "json_object"

	data, err := json.Marshal(reqBody)
	if err != nil {
		return domain.CommandResult{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return domain.CommandResult{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return domain.CommandResult{}, fmt.Errorf("call llm: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return domain.CommandResult{}, fmt.Errorf("llm returned %d", resp.StatusCode)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return domain.CommandResult{}, fmt.Errorf("decode response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return domain.CommandResult{}, fmt.Errorf("empty choices in llm response")
	}

	var raw llmResponse
	if err := json.Unmarshal([]byte(chatResp.Choices[0].Message.Content), &raw); err != nil {
		return domain.CommandResult{}, fmt.Errorf("parse llm json: %w", err)
	}

	actions := make([]domain.Action, len(raw.Actions))
	for i, a := range raw.Actions {
		actions[i] = domain.Action{
			TargetID: a.TargetID,
			Service:  a.Service,
			Data:     a.Data,
		}
	}

	return domain.CommandResult{
		Status:  domain.CommandStatus(raw.Status),
		Reply:   raw.Reply,
		Actions: actions,
	}, nil
}
