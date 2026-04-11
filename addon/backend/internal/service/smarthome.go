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

const (
	// haBGTimeout — таймаут для фоновых вызовов HA при завершении сессии.
	haBGTimeout = 15 * time.Second
	// haActionTimeout — таймаут на выполнение действий в активной сессии.
	haActionTimeout = 10 * time.Second
)

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

	// Сразу пишем основные сообщения в историю — без ожидания HA-действий.
	if !result.EndSession {
		s.appendHistory(sessionID, requestTime, []domain.ChatMessage{
			{Role: "user", Content: command},
			{Role: "assistant", Content: result.Reply},
		})
	}

	// Постобработка (HA-действия, результаты, память) — в фоне, не задерживает ответ.
	go s.postProcess(sessionID, userID, requestTime, result)

	return result, nil
}

// postProcess выполняет HA-действия, добавляет их результаты в историю
// и обновляет долгосрочную память. Запускается в отдельной горутине.
func (s *SmartHomeService) postProcess(sessionID, userID string, requestTime time.Time, result domain.CommandResult) {
	if result.Status == domain.CommandReject {
		s.log.Warn("command rejected", "session", sessionID, "reply", result.Reply)
	}

	if result.Status == domain.CommandOK && len(result.Actions) > 0 {
		if result.EndSession {
			s.dispatchActions(result.Actions)
		} else {
			actionCtx, cancel := context.WithTimeout(context.Background(), haActionTimeout)
			defer cancel()
			actionResults := s.executeActions(actionCtx, result.Actions)
			// Результаты действий кладём после основных сообщений (+1ns гарантирует порядок).
			s.appendHistory(sessionID, requestTime.Add(time.Nanosecond), []domain.ChatMessage{
				{Role: "user", Content: formatActionResults(actionResults)},
			})
		}
	}

	if result.EndSession {
		s.clearHistory(sessionID)
	}

	bg := context.Background()
	if len(result.Remember) > 0 {
		if err := s.memoryRepo.AddFacts(bg, userID, result.Remember); err != nil {
			s.log.Warn("add facts failed", "user", userID, "err", err)
		} else {
			s.log.Info("facts remembered", "user", userID, "count", len(result.Remember))
		}
	}
	if len(result.Forget) > 0 {
		if err := s.memoryRepo.ForgetFacts(bg, userID, result.Forget); err != nil {
			s.log.Warn("forget facts failed", "user", userID, "err", err)
		} else {
			s.log.Info("facts forgotten", "user", userID, "count", len(result.Forget))
		}
	}
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

// dispatchActions запускает фоновую горутину (для EndSession=true, где история не нужна).
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
