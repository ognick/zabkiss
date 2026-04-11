package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockLogger struct {
	errors []string
}

func (m *mockLogger) Info(msg string, args ...any)      {}
func (m *mockLogger) Error(msg string, args ...any)     { m.errors = append(m.errors, msg) }
func (m *mockLogger) Debug(msg string, args ...any)     {}
func (m *mockLogger) Warn(msg string, args ...any)      {}
func (m *mockLogger) Infof(format string, args ...any)  {}
func (m *mockLogger) Errorf(format string, args ...any) { m.errors = append(m.errors, format) }

func TestRecoveryMiddleware_NoPanic(t *testing.T) {
	log := &mockLogger{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	RecoveryMiddleware(log)(next).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	if len(log.errors) != 0 {
		t.Errorf("expected no errors logged, got %v", log.errors)
	}
}

func TestRecoveryMiddleware_Panic(t *testing.T) {
	log := &mockLogger{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	})
	RecoveryMiddleware(log)(next).ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if len(log.errors) == 0 {
		t.Error("expected panic to be logged")
	}
}
