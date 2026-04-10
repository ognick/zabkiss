package alice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ognick/zabkiss/internal/domain"
	"github.com/ognick/zabkiss/pkg/logger"
)

var (
	errReadBody  = errors.New("не удалось прочитать запрос")
	errParseBody = errors.New("не удалось разобрать запрос")
	errAuth      = errors.New("пожалуйста авторизуйтесь для продолжения")
)

type echoSrv interface {
	Say(text string) (string, error)
}

type userResolver interface {
	ResolveUser(ctx context.Context, token string) (domain.User, error)
}

type Handler struct {
	echoSrv       echoSrv
	auth          userResolver
	log           logger.Logger
	allowedEmails []string
}

func New(echoSrv echoSrv, auth userResolver, log logger.Logger, allowedEmails []string) *Handler {
	return &Handler{echoSrv: echoSrv, auth: auth, log: log, allowedEmails: allowedEmails}
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

	user, err := h.resolveAuth(r.Context(), req)
	if err != nil {
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

	if !h.isAllowed(user.Email) {
		msg := fmt.Sprintf("%s, у вас нет доступа", user.Name)
		h.write(w, aliceResponse{
			Version:  version,
			Response: responseBody{Text: msg, TTS: msg},
		})
		return
	}

	text, err := h.echoSrv.Say(req.Request.Command)
	if err != nil {
		h.writeError(w, fmt.Errorf("%s, произошла ошибка", user.Name))
		return
	}

	h.write(w, aliceResponse{
		Version:  version,
		Response: responseBody{Text: text, TTS: text},
	})
}

func (h *Handler) isAllowed(email string) bool {
	for _, e := range h.allowedEmails {
		if e == email {
			return true
		}
	}
	return false
}

func (h *Handler) resolveAuth(ctx context.Context, req aliceRequest) (domain.User, error) {
	token := req.Session.User.AccessToken
	yandexID := req.Session.User.UserID
	if token == "" || yandexID == "" {
		return domain.User{}, errAuth
	}
	user, err := h.auth.ResolveUser(ctx, token)
	if err != nil {
		return domain.User{}, fmt.Errorf("resolve user: %w", err)
	}
	return user, nil
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
