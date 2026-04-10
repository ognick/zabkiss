package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

type Server struct {
	*http.Server
}

func New(addr string, handler http.Handler) *Server {
	return &Server{Server: &http.Server{
		Addr:    addr,
		Handler: handler,
	}}
}

func (s *Server) waitForReady(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(1 * time.Millisecond):
		conn, err := net.Dial("tcp", s.Addr)
		if err != nil {
			return err
		}
		return conn.Close()
	}
}

func (s *Server) Run(ctx context.Context, readinessProbe func(error)) error {
	done := make(chan error)
	go func() {
		if err := s.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			done <- fmt.Errorf("http server error: %w", err)
		}
		close(done)
	}()

	go func() {
		readinessProbe(s.waitForReady(ctx))
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if err := s.Shutdown(context.Background()); err != nil {
			return fmt.Errorf("http server shutdown: %w", err)
		}
	}

	return nil
}
