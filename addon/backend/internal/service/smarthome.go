package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ognick/zabkiss/internal/domain"
	"github.com/ognick/zabkiss/pkg/logger"
)

// haActionTimeout — таймаут на выполнение HA-действий.
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

// historyEntry — группа сообщений с меткой времени запроса для сортировки.
type historyEntry struct {
	requestTime time.Time
	msgs        []domain.ChatMessage
}

// SmartHomeService оркестрирует выполнение голосовых команд умного дома.
type SmartHomeService struct {
	ha         haGateway
	llm        llmGateway
	policy     policyGateway
	memoryRepo memoryGateway
	log        logger.Logger

	sessionMu      sync.Mutex
	sessionHistory map[string][]historyEntry
}

func New(ha haGateway, llm llmGateway, policy policyGateway, memoryRepo memoryGateway, log logger.Logger) *SmartHomeService {
	return &SmartHomeService{
		ha:             ha,
		llm:            llm,
		policy:         policy,
		memoryRepo:     memoryRepo,
		log:            log,
		sessionHistory: make(map[string][]historyEntry),
	}
}

// Process выполняет голосовую команду пользователя в рамках сессии.
//
// LLM запускается в отдельной горутине с собственным фоновым контекстом
// (llmBGTimeout), независимым от Alice-дедлайна. Если LLM вернул ответ
// до дедлайна — результат возвращается немедленно, постобработка (HA-действия,
// память, история) идёт в фоне. Если дедлайн Alice истёк раньше — Process
// возвращает ошибку, LLM-горутина продолжает работу и по завершении сохраняет
// результат в историю с временной меткой исходного запроса.
func (s *SmartHomeService) Process(ctx context.Context, sessionID, userID, command string) (domain.CommandResult, error) {
	requestTime := time.Now()

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
	s.log.Info("llm response", "status", result.Status, "reply", result.Reply,
		"actions", len(result.Actions), "remember", len(result.Remember), "forget", len(result.Forget))

	// Память обновляем синхронно — влияет на следующий запрос.
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

	// Сразу пишем основные сообщения в историю — без ожидания HA-действий.
	if !result.EndSession {
		s.appendHistory(sessionID, requestTime, []domain.ChatMessage{
			{Role: "user", Content: command},
			{Role: "assistant", Content: result.Reply},
		})
	}

	// HA-действия и их результаты в историю — в фоне, не задерживает ответ.
	go s.postProcess(sessionID, requestTime, result)

	return result, nil
}

// postProcess выполняет HA-действия и добавляет их результаты в историю.
// Запускается в отдельной горутине.
func (s *SmartHomeService) postProcess(sessionID string, requestTime time.Time, result domain.CommandResult) {
	if result.Status != domain.CommandOK || len(result.Actions) == 0 {
		return
	}
	actionCtx, cancel := context.WithTimeout(context.Background(), haActionTimeout)
	defer cancel()
	actionResults := s.executeActions(actionCtx, result.Actions)
	// Результаты кладём после основных сообщений (+1ns гарантирует порядок).
	s.appendHistory(sessionID, requestTime.Add(time.Nanosecond), []domain.ChatMessage{
		{Role: "user", Content: formatActionResults(actionResults)},
	})
}

// ── HA actions ────────────────────────────────────────────────────────────────

type actionResult struct {
	TargetID string
	Service  string
	Err      error
}

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

// appendHistory добавляет группу сообщений с меткой requestTime и сортирует историю.
func (s *SmartHomeService) appendHistory(sessionID string, requestTime time.Time, msgs []domain.ChatMessage) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	entries := append(s.sessionHistory[sessionID], historyEntry{
		requestTime: requestTime,
		msgs:        msgs,
	})
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].requestTime.Before(entries[j].requestTime)
	})
	s.sessionHistory[sessionID] = entries
}

// getHistory возвращает все сообщения сессии, отсортированные по времени запроса.
func (s *SmartHomeService) getHistory(sessionID string) []domain.ChatMessage {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	entries := s.sessionHistory[sessionID]
	if len(entries) == 0 {
		return nil
	}
	var all []domain.ChatMessage
	for _, e := range entries {
		all = append(all, e.msgs...)
	}
	return all
}

func (s *SmartHomeService) clearHistory(sessionID string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	delete(s.sessionHistory, sessionID)
}
