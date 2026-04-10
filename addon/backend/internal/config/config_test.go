package config

import (
	"testing"
)

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		fallback string
		want     string
	}{
		{name: "returns env value", envVal: "hello", fallback: "fallback", want: "hello"},
		{name: "returns fallback when unset", envVal: "", fallback: "fallback", want: "fallback"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_ZK_KEY", tc.envVal)
			if got := getEnv("TEST_ZK_KEY", tc.fallback); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantAddr string
		wantDB   string
		wantLog  string
	}{
		{
			name:     "defaults",
			wantAddr: ":8080",
			wantDB:   "zabkiss.db",
			wantLog:  "debug",
		},
		{
			name:     "custom values",
			env:      map[string]string{"ADDR": ":9090", "DB_PATH": "/tmp/test.db", "LOG_LEVEL": "warn"},
			wantAddr: ":9090",
			wantDB:   "/tmp/test.db",
			wantLog:  "warn",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Ensure all keys start cleared for this subtest.
			for _, k := range []string{"ADDR", "DB_PATH", "LOG_LEVEL"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg := Load()
			if cfg.Addr != tc.wantAddr {
				t.Errorf("Addr: got %q, want %q", cfg.Addr, tc.wantAddr)
			}
			if cfg.DBPath != tc.wantDB {
				t.Errorf("DBPath: got %q, want %q", cfg.DBPath, tc.wantDB)
			}
			if cfg.LogLevel != tc.wantLog {
				t.Errorf("LogLevel: got %q, want %q", cfg.LogLevel, tc.wantLog)
			}
		})
	}
}
