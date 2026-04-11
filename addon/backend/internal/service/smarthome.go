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
	"github.com/ognick/zabkiss/pkg/youtube"
)

// youtubeSearchService — виртуальный HA-сервис для поиска и воспроизведения YouTube.
const youtubeSearchService = "media_player.play_youtube"

const (
	// haActionTimeout — таймаут на выполнение HA-действий.
	haActionTimeout = 10 * time.Second
	// llmBGTimeout — таймаут фоновой обработки (LLM + HA) независимо от Alice-дедлайна.
	llmBGTimeout = 30 * time.Second
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

// YouTubeGateway — интерфейс для поиска видео на YouTube.
type YouTubeGateway interface {
	SearchVideo(ctx context.Context, query string) (youtube.SearchResult, error)
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
	youtube    YouTubeGateway // nil если YouTube API не настроен
	log        logger.Logger

	sessionMu      sync.Mutex
	sessionHistory map[string][]historyEntry
	sessionInbox   map[string]string // отложенный ответ для следующего запроса
}

func New(ha haGateway, llm llmGateway, policy policyGateway, memoryRepo memoryGateway, yt YouTubeGateway, log logger.Logger) *SmartHomeService {
	return &SmartHomeService{
		ha:             ha,
		llm:            llm,
		policy:         policy,
		memoryRepo:     memoryRepo,
		youtube:        yt,
		log:            log,
		sessionHistory: make(map[string][]historyEntry),
		sessionInbox:   make(map[string]string),
	}
}

// processResult — результат полной обработки команды (LLM + HA).
type processResult struct {
	result domain.CommandResult
	err    error
}

// Process выполняет голосовую команду пользователя в рамках сессии.
//
// LLM + HA-действия выполняются последовательно в одной горутине с собственным
// фоновым контекстом (llmBGTimeout). Если вся цепочка завершилась до Alice-дедлайна —
// результат возвращается сразу. Иначе возвращается «Обрабатываю запрос, спроси чуть
// позже», а когда горутина завершится — результат сохраняется в inbox сессии и
// prepend-ится к ответу на следующий запрос.
func (s *SmartHomeService) Process(ctx context.Context, sessionID, userID, command string) (domain.CommandResult, error) {
	requestTime := time.Now()

	s.log.Info("process command", "session", sessionID, "user", userID, "command", command)

	// Забираем отложенный ответ от предыдущего запроса (если он завершился после таймаута).
	pendingReply := s.popInbox(sessionID)

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

	if s.youtube != nil {
		devices = injectYouTubeService(devices)
	}

	memFacts, err := s.memoryRepo.GetFacts(ctx, userID)
	if err != nil {
		s.log.Warn("memory fetch failed", "user", userID, "err", err)
	}
	s.log.Debug("user memory facts", "user", userID, "count", len(memFacts))

	history := s.getHistory(sessionID)
	s.log.Debug("session history", "session", sessionID, "messages", len(history))

	resultCh := make(chan processResult, 1)
	bgCtx, bgCancel := context.WithTimeout(context.Background(), llmBGTimeout)

	go func() {
		defer bgCancel()
		resultCh <- s.runLLMAndHA(bgCtx, sessionID, userID, command, requestTime, devices, history, memFacts)
	}()

	select {
	case r := <-resultCh:
		if r.err != nil {
			return domain.CommandResult{}, r.err
		}
		reply := r.result.Reply
		if pendingReply != "" {
			reply = pendingReply + ". " + reply
			r.result.Reply = reply
		}
		return r.result, nil

	case <-ctx.Done():
		// Alice-дедлайн истёк раньше чем завершилась обработка.
		// Горутина продолжает работу; когда закончит — кладёт результат в inbox.
		s.log.Warn("alice deadline exceeded, deferring result", "session", sessionID)
		go func() {
			select {
			case r := <-resultCh:
				if r.err != nil {
					s.log.Error("deferred processing failed", "session", sessionID, "err", r.err)
					return
				}
				s.log.Info("deferred result ready, storing in inbox", "session", sessionID)
				s.storeInbox(sessionID, r.result.Reply)
			case <-bgCtx.Done():
				s.log.Warn("deferred processing timed out", "session", sessionID)
			}
		}()

		deferredMsg := "Обрабатываю запрос, спроси чуть позже"
		if pendingReply != "" {
			deferredMsg = pendingReply + ". " + deferredMsg
		}
		return domain.CommandResult{
			Status: domain.CommandOK,
			Reply:  deferredMsg,
		}, nil
	}
}

// runLLMAndHA выполняет LLM-запрос, обновляет память и выполняет HA-действия последовательно.
// Возвращает итоговый CommandResult (с учётом ошибок HA).
func (s *SmartHomeService) runLLMAndHA(
	ctx context.Context,
	sessionID, userID, command string,
	requestTime time.Time,
	devices []domain.Device,
	history []domain.ChatMessage,
	memFacts []domain.MemoryFact,
) processResult {
	result, err := s.llm.Execute(ctx, command, devices, history, memFacts)
	if err != nil {
		s.log.Error("llm execute failed", "err", err)
		return processResult{err: err}
	}
	s.log.Info("llm response", "status", result.Status, "reply", result.Reply,
		"actions", len(result.Actions), "remember", len(result.Remember), "forget", len(result.Forget))

	// Обновляем память — влияет на следующий запрос.
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

	// Выполняем HA-действия последовательно.
	if result.Status == domain.CommandOK && len(result.Actions) > 0 {
		actionCtx, cancel := context.WithTimeout(ctx, haActionTimeout)
		defer cancel()

		actionResults := s.executeActions(actionCtx, result.Actions)

		if allFailed(actionResults) {
			result.Reply = "Не удалось выполнить команду — устройство не ответило или недоступно"
		} else if hasFailed(actionResults) {
			result.Reply += ". Часть команд не удалось выполнить — устройство не ответило"
		}

		// Результаты действий кладём в историю после основных сообщений.
		s.appendHistory(sessionID, requestTime.Add(time.Nanosecond), []domain.ChatMessage{
			{Role: "user", Content: formatActionResults(actionResults)},
		})
	}

	// Основные сообщения диалога.
	if !result.EndSession {
		s.appendHistory(sessionID, requestTime, []domain.ChatMessage{
			{Role: "user", Content: command},
			{Role: "assistant", Content: result.Reply},
		})
	}

	return processResult{result: result}
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
		var err error
		if action.Service == youtubeSearchService && s.youtube != nil {
			err = s.executeYouTubeAction(ctx, action)
		} else {
			err = s.ha.CallService(ctx, action.TargetID, action.Service, action.Data)
		}
		results[i] = actionResult{TargetID: action.TargetID, Service: action.Service, Err: err}
		if err != nil {
			s.log.Error("ha action failed", "err", err, "target", action.TargetID, "service", action.Service)
		}
	}
	return results
}

// executeYouTubeAction ищет видео по запросу и запускает его через media_player.play_media (Cast).
func (s *SmartHomeService) executeYouTubeAction(ctx context.Context, action domain.Action) error {
	query, _ := action.Data["query"].(string)
	if query == "" {
		return fmt.Errorf("play_youtube: empty query")
	}
	video, err := s.youtube.SearchVideo(ctx, query)
	if err != nil {
		return fmt.Errorf("play_youtube search: %w", err)
	}
	s.log.Info("youtube search result", "query", query, "video_id", video.VideoID, "title", video.Title)

	mediaID := fmt.Sprintf(`{"app_name":"youtube","media_id":"%s"}`, video.VideoID)
	return s.ha.CallService(ctx, action.TargetID, "media_player.play_media", map[string]any{
		"media_content_type": "cast",
		"media_content_id":   mediaID,
	})
}

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

func hasFailed(results []actionResult) bool {
	for _, r := range results {
		if r.Err != nil {
			return true
		}
	}
	return false
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

// injectYouTubeService добавляет виртуальный сервис play_youtube ко всем доступным media_player устройствам.
func injectYouTubeService(devices []domain.Device) []domain.Device {
	youtubeVirtualSvc := domain.DeviceService{
		Service: youtubeSearchService,
		Params: map[string]domain.DeviceParam{
			"query": {Type: domain.ParamTypeString},
		},
	}
	result := make([]domain.Device, len(devices))
	copy(result, devices)
	for i, d := range result {
		if !strings.HasPrefix(d.EntityID, "media_player.") || len(d.Services) == 0 {
			continue
		}
		svcsCopy := make([]domain.DeviceService, len(d.Services), len(d.Services)+1)
		copy(svcsCopy, d.Services)
		result[i].Services = append(svcsCopy, youtubeVirtualSvc)
	}
	return result
}

// ── session inbox ─────────────────────────────────────────────────────────────

// popInbox возвращает отложенный ответ сессии и очищает его.
func (s *SmartHomeService) popInbox(sessionID string) string {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	reply := s.sessionInbox[sessionID]
	delete(s.sessionInbox, sessionID)
	return reply
}

// storeInbox сохраняет отложенный ответ в inbox сессии (append если уже есть).
func (s *SmartHomeService) storeInbox(sessionID, reply string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if existing := s.sessionInbox[sessionID]; existing != "" {
		s.sessionInbox[sessionID] = existing + ". " + reply
	} else {
		s.sessionInbox[sessionID] = reply
	}
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
