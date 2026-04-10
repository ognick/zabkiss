package config

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Addr          string
	DBPath        string
	LogLevel      string
	OpenAIAPIKey  string
	AllowedEmails []string
}

func Load() *Config {
	_ = godotenv.Load()
	return &Config{
		Addr:          getEnv("ADDR", ":8080"),
		DBPath:        getEnv("DB_PATH", "zabkiss.db"),
		LogLevel:      getEnv("LOG_LEVEL", "debug"),
		OpenAIAPIKey:  getEnv("OPENAI_API_KEY", ""),
		AllowedEmails: parseList(getEnv("ALLOWED_EMAILS", "")),
	}
}

func parseList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
