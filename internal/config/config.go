package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	SpeachesURL       string
	ListenAddr        string
	WhisperModel      string
	WhisperLanguage   string
	TelegramToken     string
	TelegramChatID    int64
	EvolutionURL      string
	EvolutionAPIKey   string
	EvolutionInstance string
	OwnerPhone        string
	OllamaURL         string
	OllamaModel       string
	OllamaConcurrency int
	DatabaseURL       string
	InstanceID        string
	MuteGroups        bool
	MutedJIDs         map[string]bool
}

func Load() (*Config, error) {
	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid TELEGRAM_CHAT_ID %q: %w", chatIDStr, err)
	}

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	return &Config{
		SpeachesURL:       envOrDefault("SPEACHES_URL", "http://speaches:8000"),
		ListenAddr:        envOrDefault("LISTEN_ADDR", ":8080"),
		WhisperModel:      envOrDefault("WHISPER_MODEL", "large-v3"),
		WhisperLanguage:   envOrDefault("WHISPER_LANGUAGE", "pt"),
		TelegramToken:     token,
		TelegramChatID:    chatID,
		EvolutionURL:      os.Getenv("EVOLUTION_API_URL"),
		EvolutionAPIKey:   os.Getenv("EVOLUTION_API_KEY"),
		EvolutionInstance: envOrDefault("EVOLUTION_INSTANCE", "barry"),
		OwnerPhone:        os.Getenv("OWNER_PHONE"),
		OllamaURL:         envOrDefault("OLLAMA_URL", ""),
		OllamaModel:       envOrDefault("OLLAMA_MODEL", "hf.co/HauhauCS/Qwen3.5-9B-Uncensored-HauhauCS-Aggressive:Q8_0"),
		OllamaConcurrency: envOrDefaultInt("OLLAMA_CONCURRENCY", 3),
		DatabaseURL:       ensureNoSSL(os.Getenv("DATABASE_URL")),
		InstanceID:        envOrDefault("EVOLUTION_INSTANCE_ID", ""),
		MuteGroups:        os.Getenv("MUTE_GROUPS") == "true",
		MutedJIDs:         parseJIDs(os.Getenv("MUTED_JIDS")),
	}, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func ensureNoSSL(dsn string) string {
	if dsn == "" {
		return ""
	}
	if strings.Contains(dsn, "sslmode=") {
		return dsn
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&sslmode=disable"
	}
	return dsn + "?sslmode=disable"
}

func parseJIDs(raw string) map[string]bool {
	result := make(map[string]bool)
	for _, jid := range strings.Split(raw, ",") {
		jid = strings.TrimSpace(jid)
		if jid != "" {
			result[jid] = true
		}
	}
	return result
}
