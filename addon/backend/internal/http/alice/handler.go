package alice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ognick/zabkiss/internal/domain"
	"github.com/ognick/zabkiss/pkg/logger"
)

var (
	errReadBody  = errors.New("не удалось прочитать запрос")
	errParseBody = errors.New("не удалось разобрать запрос")
	errAuth      = errors.New("пожалуйста авторизуйтесь для продолжения")
)

// requestBudget — сколько времени до истечения Alice-таймаута мы резервируем
// на отправку ответа с учётом сетевой задержки и JSON-кодирования.
const requestBudget = 800 * time.Millisecond

type commandService interface {
	Process(ctx context.Context, sessionID, userID, command string) (domain.CommandResult, error)
}

type userResolver interface {
	ResolveUser(ctx context.Context, token string) (domain.User, error)
}

type Handler struct {
	svc  commandService
	auth userResolver
	log  logger.Logger
}

func New(svc commandService, auth userResolver, log logger.Logger) *Handler {
	return &Handler{svc: svc, auth: auth, log: log}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/alice", func(r chi.Router) {
		r.Post("/webhook", h.webhook)
	})
}

func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, errReadBody)
		return
	}

	var req aliceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.writeError(w, errParseBody)
		return
	}

	if req.Request.OriginalUtterance == "ping" {
		h.log.Info("health check", "session_id", req.Session.SessionID)
		h.write(w, aliceResponse{
			Version:  version,
			Response: responseBody{Text: "ok"},
		})
		return
	}

	h.log.Info("webhook request", "session", req.Session.SessionID, "utterance", req.Request.OriginalUtterance)

	user, err := h.resolveAuth(r.Context(), req)
	if err != nil {
		h.log.Warn("auth failed", "session", req.Session.SessionID, "err", err)
		if errors.Is(err, errForbidden) {
			msg := fmt.Sprintf("%s, %s", user.Name, errForbidden.Error())
			h.write(w, aliceResponse{
				Version:  version,
				Response: responseBody{Text: msg, TTS: msg},
			})
			return
		}
		if !errors.Is(err, errAuth) {
			h.log.Error(err.Error())
		}
		h.write(w, aliceResponse{
			Version: version,
			Response: responseBody{
				Text:       errAuth.Error(),
				Directives: &directives{StartAccountLinking: &struct{}{}},
			},
		})
		return
	}

	// Применяем бюджет таймаута Алисы: Request-Timeout в микросекундах.
	ctx := r.Context()
	if v := r.Header.Get("Request-Timeout"); v != "" {
		if us, err := strconv.ParseInt(v, 10, 64); err == nil && us > 0 {
			timeout := time.Duration(us) * time.Microsecond
			if timeout > requestBudget {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(timeout-requestBudget))
				defer cancel()
			}
		}
	}

	h.log.Info("auth ok", "session", req.Session.SessionID, "user", user.Name, "email", user.Email)

	result, err := h.svc.Process(ctx, req.Session.SessionID, user.ID, req.Request.Command)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			msg := fmt.Sprintf("%s, не успел обработать запрос, попробуйте ещё раз", user.Name)
			h.write(w, aliceResponse{
				Version:  version,
				Response: responseBody{Text: msg, TTS: msg, EndSession: true},
			})
			return
		}
		h.log.Error("process command", "err", err)
		h.writeError(w, fmt.Errorf("%s, произошла ошибка при обработке команды", user.Name))
		return
	}

	text := result.Reply
	h.write(w, aliceResponse{
		Version:  version,
		Response: responseBody{Text: text, TTS: text, EndSession: result.EndSession},
	})
}

func (h *Handler) resolveAuth(ctx context.Context, req aliceRequest) (domain.User, error) {
	token := req.Session.User.AccessToken
	yandexID := req.Session.User.UserID
	if token == "" || yandexID == "" {
		return domain.User{}, errAuth
	}
	return h.auth.ResolveUser(ctx, token)
}

func (h *Handler) write(w http.ResponseWriter, resp aliceResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.log.Error("encode response", "err", err)
	}
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	h.log.Error(err.Error())
	h.write(w, aliceResponse{
		Version:  version,
		Response: responseBody{Text: err.Error(), TTS: err.Error(), EndSession: true},
	})
}
