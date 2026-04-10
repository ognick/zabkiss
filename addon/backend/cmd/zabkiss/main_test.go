package main

import (
	"os"
	"strings"
	"testing"
)

func TestLocalHost_HasLocalSuffix(t *testing.T) {
	got := localHost()
	if !strings.HasSuffix(got, ".local") {
		t.Errorf("localHost() = %q, want .local suffix", got)
	}
}

func TestLocalHost_NoDoubleLocalSuffix(t *testing.T) {
	got := localHost()
	if strings.Contains(got, ".local.local") {
		t.Errorf("localHost() = %q, has double .local suffix", got)
	}
}

func TestLocalHost_FallsBackOnError(t *testing.T) {
	// If hostname already has .local, result equals hostname
	name, err := os.Hostname()
	if err != nil {
		t.Skip("cannot get hostname")
	}
	got := localHost()
	if strings.HasSuffix(name, ".local") && got != name {
		t.Errorf("localHost() = %q, should equal hostname %q when it already has .local", got, name)
	}
}
