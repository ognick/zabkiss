package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Addr                  string
	DBPath                string
	LogLevel              string
	OpenAIAPIKey          string
	LLMBaseURL            string
	LLMModel              string
	AllowedEmails         []string
	HAToken               string
	HAURL                 string
	PolicyCacheTTLSeconds int
}

func Load() *Config {
	_ = godotenv.Load()
	return &Config{
		Addr:                  getEnv("ADDR", ":8080"),
		DBPath:                getEnv("DB_PATH", "/data/zabkiss.db"),
		LogLevel:              getEnv("LOG_LEVEL", "debug"),
		OpenAIAPIKey:          getEnv("OPENAI_API_KEY", ""),
		LLMBaseURL:            getEnv("LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMModel:              getEnv("LLM_MODEL", "gpt-4o-mini"),
		AllowedEmails:         parseList(getEnv("ALLOWED_EMAILS", "")),
		HAToken:               getEnv("HA_TOKEN", ""),
		HAURL:                 getEnv("HA_URL", "http://homeassistant:8123"),
		PolicyCacheTTLSeconds: parseInt(getEnv("POLICY_CACHE_TTL_SECONDS", "10")),
	}
}

func parseList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func parseInt(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return 60
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
