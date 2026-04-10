package service

import (
	"context"
	"sync"
	"time"

	"github.com/ognick/zabkiss/internal/domain"
	"github.com/ognick/zabkiss/pkg/logger"
)

// haBGTimeout — таймаут для фоновых вызовов Home Assistant.
// Достаточно большой для обработки, но ограниченный во избежание утечек горутин.
const haBGTimeout = 15 * time.Second

// haGateway — интерфейс адаптера Home Assistant.
type haGateway interface {
	GetDeviceInfos(ctx context.Context, entityIDs []string) ([]domain.Device, error)
	CallService(ctx context.Context, entityID, service string, params map[string]any) error
}

// llmGateway — интерфейс адаптера LLM.
type llmGateway interface {
	Execute(ctx context.Context, command string, devices []domain.Device, history []domain.ChatMessage) (domain.CommandResult, error)
}

// policyGateway — интерфейс клиента политики.
type policyGateway interface {
	GetEntities(ctx context.Context) ([]string, error)
}

// SmartHomeService оркестрирует выполнение голосовых команд умного дома:
// получает список разрешённых устройств, их состояние, обращается к LLM,
// управляет историей сессии и выполняет действия в фоне.
type SmartHomeService struct {
	ha     haGateway
	llm    llmGateway
	policy policyGateway
	log    logger.Logger

	sessionMu      sync.Mutex
	sessionHistory map[string][]domain.ChatMessage // sessionID → история диалога
}

// New создаёт SmartHomeService с заданными зависимостями.
func New(ha haGateway, llm llmGateway, policy policyGateway, log logger.Logger) *SmartHomeService {
	return &SmartHomeService{
		ha:             ha,
		llm:            llm,
		policy:         policy,
		log:            log,
		sessionHistory: make(map[string][]domain.ChatMessage),
	}
}

// Process выполняет голосовую команду пользователя в рамках сессии.
// Контекст ctx должен нести дедлайн, установленный хендлером на основе Request-Timeout.
// При статусе ok запускает фоновую задачу вызова HA и возвращает управление сразу.
func (s *SmartHomeService) Process(ctx context.Context, sessionID, command string) (domain.CommandResult, error) {
	s.log.Info("process command", "session", sessionID, "command", command)

	entities, err := s.policy.GetEntities(ctx)
	if err != nil {
		s.log.Error("policy fetch failed", "err", err)
		return domain.CommandResult{}, err
	}
	s.log.Info("policy entities", "count", len(entities), "entities", entities)

	devices, err := s.ha.GetDeviceInfos(ctx, entities)
	if err != nil {
		s.log.Error("ha device fetch failed", "err", err)
		return domain.CommandResult{}, err
	}
	s.log.Info("ha devices loaded", "count", len(devices))

	history := s.getHistory(sessionID)
	s.log.Debug("session history", "session", sessionID, "messages", len(history))

	result, err := s.llm.Execute(ctx, command, devices, history)
	if err != nil {
		s.log.Error("llm execute failed", "err", err)
		return domain.CommandResult{}, err
	}
	s.log.Info("llm response", "status", result.Status, "reply", result.Reply, "actions", len(result.Actions))

	switch result.Status {
	case domain.CommandOK:
		s.clearHistory(sessionID)
		s.dispatchActions(result.Actions)

	case domain.CommandClarify:
		updated := append(history,
			domain.ChatMessage{Role: "user", Content: command},
			domain.ChatMessage{Role: "assistant", Content: result.Reply},
		)
		s.setHistory(sessionID, updated)

	default: // CommandReject
		s.log.Warn("command rejected", "session", sessionID, "reply", result.Reply)
		s.clearHistory(sessionID)
	}

	return result, nil
}

// dispatchActions запускает фоновую горутину, которая последовательно
// вызывает CallService для каждого действия с собственным таймаутом.
func (s *SmartHomeService) dispatchActions(actions []domain.Action) {
	if len(actions) == 0 {
		return
	}
	snapshot := make([]domain.Action, len(actions))
	copy(snapshot, actions)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), haBGTimeout)
		defer cancel()
		for _, action := range snapshot {
			if err := s.ha.CallService(ctx, action.TargetID, action.Service, action.Data); err != nil {
				s.log.Error("dispatch action", "err", err, "target", action.TargetID, "service", action.Service)
			}
		}
	}()
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
