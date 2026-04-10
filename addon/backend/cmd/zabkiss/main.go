package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ognick/goscade/v2"
	"github.com/ognick/zabkiss/internal/config"
	"github.com/ognick/zabkiss/internal/http/alice"
	sqliterepo "github.com/ognick/zabkiss/internal/repository/sqlite"
	"github.com/ognick/zabkiss/pkg/httpserver"
	"github.com/ognick/zabkiss/pkg/logger"
	"github.com/ognick/zabkiss/pkg/sqlitedb"
)

func main() {
	cfg := config.Load()

	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}
	slogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	log := logger.NewSlogAdapter(slogger)

	db, err := sqlitedb.New(cfg.DBPath)
	if err != nil {
		log.Error("db", "err", err)
		return
	}

	userRepo, err := sqliterepo.NewUserRepo(db.DB)
	if err != nil {
		log.Error("user repo", "err", err)
		return
	}

	r := chi.NewRouter()
	r.Use(httpserver.RecoveryMiddleware(log))
	r.Use(middleware.Logger)
	if level == slog.LevelDebug {
		r.Use(httpserver.DebugMiddleware())
	}
	alice.New(&echoStub{}, alice.NewAuth(userRepo), log, cfg.AllowedEmails).Register(r)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	host := localHost()
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		log.Info(method + " http://" + host + cfg.Addr + route)
		return nil
	}); err != nil {
		log.Error("failed to walk routes", "err", err)
	}

	lc := goscade.NewLifecycle(log, goscade.WithShutdownHook())
	goscade.Register(lc, db)
	goscade.Register(lc, httpserver.New(cfg.Addr, r), db)

	if err := goscade.Run(context.Background(), lc, func() {
		log.Info("ZabKiss ready", "addr", cfg.Addr)
	}); err != nil {
		log.Error("fatal", "err", err)
	}
}

type echoStub struct{}

func (s *echoStub) Say(text string) (string, error) {
	return fmt.Sprintf("ты говоришь - %s, но пока нахожусь в режиме разработки.", text), nil
}

func localHost() string {
	name, err := os.Hostname()
	if err != nil {
		return "localhost"
	}
	if strings.HasSuffix(name, ".local") {
		return name
	}
	return name + ".local"
}
