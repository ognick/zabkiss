package httpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.status = code
	rr.ResponseWriter.WriteHeader(code)
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	rr.body.Write(b)
	return rr.ResponseWriter.Write(b)
}

func DebugMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqBody, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(reqBody))

			var sb strings.Builder
			fmt.Fprintf(&sb, "\n── REQUEST ▶ %s %s\n", r.Method, r.URL.String())
			for k, v := range r.Header {
				fmt.Fprintf(&sb, "  %s: %s\n", k, strings.Join(v, ", "))
			}
			if len(reqBody) > 0 {
				fmt.Fprintf(&sb, "%s\n", prettyJSON(reqBody))
			}
			fmt.Fprint(os.Stderr, sb.String())

			rr := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rr, r)

			sb.Reset()
			fmt.Fprintf(&sb, "\n── RESPONSE ◀ %d\n", rr.status)
			for k, v := range w.Header() {
				fmt.Fprintf(&sb, "  %s: %s\n", k, strings.Join(v, ", "))
			}
			if rr.body.Len() > 0 {
				fmt.Fprintf(&sb, "%s\n", prettyJSON(rr.body.Bytes()))
			}
			fmt.Fprint(os.Stderr, sb.String())
		})
	}
}

func prettyJSON(b []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return string(b)
	}
	return buf.String()
}
