package httpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrettyJSON_ValidJSON(t *testing.T) {
	input := []byte(`{"key":"value","num":42}`)
	got := prettyJSON(input)
	if !strings.Contains(got, "\n") {
		t.Errorf("expected pretty-printed JSON with newlines, got: %q", got)
	}
	if !strings.Contains(got, `"key"`) {
		t.Errorf("expected key in output, got: %q", got)
	}
}

func TestPrettyJSON_InvalidJSON(t *testing.T) {
	input := []byte(`not json`)
	got := prettyJSON(input)
	if got != "not json" {
		t.Errorf("expected raw string for invalid JSON, got: %q", got)
	}
}

func TestPrettyJSON_Empty(t *testing.T) {
	got := prettyJSON([]byte{})
	if got != "" {
		t.Errorf("expected empty string, got: %q", got)
	}
}

func TestDebugMiddleware_PassesThrough(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"hello":"world"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	})
	DebugMiddleware()(next).ServeHTTP(w, r)

	if !called {
		t.Error("expected next handler to be called")
	}
	if w.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestDebugMiddleware_BodyReadableByNext(t *testing.T) {
	body := `{"key":"value"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	var readBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		readBody = string(b)
	})
	DebugMiddleware()(next).ServeHTTP(w, r)

	if readBody != body {
		t.Errorf("body in next: got %q, want %q", readBody, body)
	}
}
