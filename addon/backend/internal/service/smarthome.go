package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ognick/zabkiss/internal/domain"
	"github.com/ognick/zabkiss/pkg/logger"
)

// haBGTimeout — таймаут для фоновых вызовов Home Assistant при завершении сессии.
const haBGTimeout = 15 * time.Second

// haActionTimeout — таймаут на выполнение действий внутри активной сессии.
const haActionTimeout = 10 * time.Second

type haGateway interface {
	GetDeviceInfos(ctx context.Context, entityIDs []string) ([]domain.Device, error)
	CallService(ctx context.Context, entityID, service string, params map[string]any) error
}

type llmGateway interface {
	Execute(ctx context.Context, command string, devices []domain.Device, history []domain.ChatMessage, memoryFacts []domain.MemoryFact) (domain.CommandResult, error)
}

type policyGateway interface {
	GetEntities(ctx context.Context) ([]string, error)
}

type memoryGateway interface {
	GetFacts(ctx context.Context, userID string) ([]domain.MemoryFact, error)
	AddFacts(ctx context.Context, userID string, facts []string) error
	ForgetFacts(ctx context.Context, userID string, factIDs []string) error
}

// SmartHomeService оркестрирует выполнение голосовых команд умного дома.
type SmartHomeService struct {
	ha         haGateway
	llm        llmGateway
	policy     policyGateway
	memoryRepo memoryGateway
	log        logger.Logger

	sessionMu      sync.Mutex
	sessionHistory map[string][]domain.ChatMessage
}

func New(ha haGateway, llm llmGateway, policy policyGateway, memoryRepo memoryGateway, log logger.Logger) *SmartHomeService {
	return &SmartHomeService{
		ha:             ha,
		llm:            llm,
		policy:         policy,
		memoryRepo:     memoryRepo,
		log:            log,
		sessionHistory: make(map[string][]domain.ChatMessage),
	}
}

// Process выполняет голосовую команду пользователя в рамках сессии.
func (s *SmartHomeService) Process(ctx context.Context, sessionID, userID, command string) (domain.CommandResult, error) {
	s.log.Info("process command", "session", sessionID, "user", userID, "command", command)

	entities, err := s.policy.GetEntities(ctx)
	if err != nil {
		s.log.Error("policy fetch failed", "err", err)
		return domain.CommandResult{}, err
	}
	s.log.Info("policy entities", "count", len(entities))

	devices, err := s.ha.GetDeviceInfos(ctx, entities)
	if err != nil {
		s.log.Error("ha device fetch failed", "err", err)
		return domain.CommandResult{}, err
	}
	s.log.Info("ha devices loaded", "count", len(devices))

	memFacts, err := s.memoryRepo.GetFacts(ctx, userID)
	if err != nil {
		s.log.Warn("memory fetch failed", "user", userID, "err", err)
		memFacts = nil
	}
	s.log.Debug("user memory facts", "user", userID, "count", len(memFacts))

	history := s.getHistory(sessionID)
	s.log.Debug("session history", "session", sessionID, "messages", len(history))

	result, err := s.llm.Execute(ctx, command, devices, history, memFacts)
	if err != nil {
		s.log.Error("llm execute failed", "err", err)
		return domain.CommandResult{}, err
	}
	s.log.Info("llm response", "status", result.Status, "reply", result.Reply, "actions", len(result.Actions), "remember", len(result.Remember), "forget", len(result.Forget))

	msgs := append(history,
		domain.ChatMessage{Role: "user", Content: command},
		domain.ChatMessage{Role: "assistant", Content: result.Reply},
	)

	if result.Status == domain.CommandOK && len(result.Actions) > 0 {
		if result.EndSession {
			// Сессия завершается — результаты истории не нужны, запускаем фоново.
			s.dispatchActions(result.Actions)
		} else {
			actionCtx, cancel := context.WithTimeout(context.Background(), haActionTimeout)
			defer cancel()
			actionResults := s.executeActions(actionCtx, result.Actions)
			if allFailed(actionResults) {
				result.Reply = "Не удалось выполнить команду — устройство не ответило или недоступно"
			}
			msgs = append(msgs, domain.ChatMessage{
				Role:    "user",
				Content: formatActionResults(actionResults),
			})
		}
	}

	if result.Status == domain.CommandReject {
		s.log.Warn("command rejected", "session", sessionID, "reply", result.Reply)
	}

	if !result.EndSession {
		s.setHistory(sessionID, msgs)
	} else {
		s.clearHistory(sessionID)
	}

	if len(result.Remember) > 0 {
		if err := s.memoryRepo.AddFacts(ctx, userID, result.Remember); err != nil {
			s.log.Warn("add facts failed", "user", userID, "err", err)
		} else {
			s.log.Info("facts remembered", "user", userID, "count", len(result.Remember))
		}
	}
	if len(result.Forget) > 0 {
		if err := s.memoryRepo.ForgetFacts(ctx, userID, result.Forget); err != nil {
			s.log.Warn("forget facts failed", "user", userID, "err", err)
		} else {
			s.log.Info("facts forgotten", "user", userID, "count", len(result.Forget))
		}
	}

	return result, nil
}

type actionResult struct {
	TargetID string
	Service  string
	Err      error
}

// executeActions выполняет действия синхронно и возвращает результаты.
func (s *SmartHomeService) executeActions(ctx context.Context, actions []domain.Action) []actionResult {
	results := make([]actionResult, len(actions))
	for i, action := range actions {
		err := s.ha.CallService(ctx, action.TargetID, action.Service, action.Data)
		results[i] = actionResult{TargetID: action.TargetID, Service: action.Service, Err: err}
		if err != nil {
			s.log.Error("ha action failed", "err", err, "target", action.TargetID, "service", action.Service)
		}
	}
	return results
}

// dispatchActions запускает фоновую горутину (используется при EndSession=true).
func (s *SmartHomeService) dispatchActions(actions []domain.Action) {
	if len(actions) == 0 {
		return
	}
	snapshot := make([]domain.Action, len(actions))
	copy(snapshot, actions)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), haBGTimeout)
		defer cancel()
		s.executeActions(ctx, snapshot)
	}()
}

// allFailed возвращает true если все действия завершились с ошибкой.
func allFailed(results []actionResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, r := range results {
		if r.Err == nil {
			return false
		}
	}
	return true
}

// formatActionResults формирует сообщение для истории диалога с результатами действий.
func formatActionResults(results []actionResult) string {
	var sb strings.Builder
	sb.WriteString("[Результаты выполненных действий]\n")
	for _, r := range results {
		if r.Err != nil {
			sb.WriteString(fmt.Sprintf("- %s %s: ошибка — %s\n", r.Service, r.TargetID, r.Err))
		} else {
			sb.WriteString(fmt.Sprintf("- %s %s: успешно\n", r.Service, r.TargetID))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ── session history ───────────────────────────────────────────────────────────

func (s *SmartHomeService) getHistory(sessionID string) []domain.ChatMessage {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	h := s.sessionHistory[sessionID]
	if len(h) == 0 {
		return nil
	}
	cp := make([]domain.ChatMessage, len(h))
	copy(cp, h)
	return cp
}

func (s *SmartHomeService) setHistory(sessionID string, history []domain.ChatMessage) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.sessionHistory[sessionID] = history
}

func (s *SmartHomeService) clearHistory(sessionID string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	delete(s.sessionHistory, sessionID)
}
