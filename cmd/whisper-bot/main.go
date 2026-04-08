package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"

	"github.com/barrymac/whisper-watch/internal/api"
	"github.com/barrymac/whisper-watch/internal/bot"
	"github.com/barrymac/whisper-watch/internal/config"
	"github.com/barrymac/whisper-watch/internal/contacts"
	"github.com/barrymac/whisper-watch/internal/evolution"
	"github.com/barrymac/whisper-watch/internal/filters"
	"github.com/barrymac/whisper-watch/internal/ollama"
	"github.com/barrymac/whisper-watch/internal/state"
	"github.com/barrymac/whisper-watch/internal/whisper"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	whisperClient := whisper.NewClient(cfg.SpeachesURL, cfg.WhisperModel, cfg.WhisperLanguage)

	var evolutionClient *evolution.Client
	if cfg.EvolutionURL != "" && cfg.EvolutionAPIKey != "" {
		evolutionClient = evolution.NewClient(cfg.EvolutionURL, cfg.EvolutionAPIKey, cfg.EvolutionInstance)
		slog.Info("evolution API client configured", "url", cfg.EvolutionURL, "instance", cfg.EvolutionInstance)
	} else {
		slog.Info("evolution API not configured, WhatsApp webhook disabled")
	}

	var ollamaClient *ollama.Client
	if cfg.OllamaURL != "" {
		ollamaClient = ollama.NewClient(cfg.OllamaURL, cfg.OllamaModel)
		slog.Info("ollama client configured", "url", cfg.OllamaURL, "model", cfg.OllamaModel)
	} else {
		slog.Info("ollama not configured, using Whisper built-in translation")
	}

	var contactStore *contacts.Store
	var stateStore *state.Store

	if cfg.DatabaseURL != "" && cfg.InstanceID != "" {
		contactStore, err = contacts.NewStore(cfg.DatabaseURL, cfg.InstanceID)
		if err != nil {
			slog.Error("failed to connect to database", "error", err)
		} else {
			slog.Info("contacts store connected", "instanceId", cfg.InstanceID)
			defer contactStore.Close()

			stateStore, err = state.NewStore(contactStore.DB())
			if err != nil {
				slog.Error("failed to init state store", "error", err)
				stateStore = nil
			}
		}
	} else {
		slog.Info("database not configured, contact lookup and state persistence disabled")
	}

	defaults := state.Settings{
		MuteGroups:     cfg.MuteGroups,
		TranslateAudio: true,
		TranslateText:  true,
		ReplyDrafts:    true,
		OllamaModel:    cfg.OllamaModel,
		MutedJIDs:      cfg.MutedJIDs,
	}

	var loaded state.Settings
	if stateStore != nil {
		loaded, err = stateStore.Load(defaults)
		if err != nil {
			slog.Warn("failed to load persisted state, using defaults", "error", err)
			loaded = defaults
		}
	} else {
		loaded = defaults
	}

	f := filters.New(loaded.MuteGroups, loaded.MutedJIDs, loaded.OllamaModel)
	f.SetTranslateAudio(loaded.TranslateAudio)
	f.SetTranslateText(loaded.TranslateText)
	f.SetReplyDrafts(loaded.ReplyDrafts)

	slog.Info("filter config",
		"muteGroups", loaded.MuteGroups,
		"mutedJIDs", len(loaded.MutedJIDs),
		"translateAudio", loaded.TranslateAudio,
		"translateText", loaded.TranslateText,
		"replyDrafts", loaded.ReplyDrafts,
		"ollamaModel", loaded.OllamaModel,
	)

	telegramBot, err := bot.New(cfg.TelegramToken, cfg.TelegramChatID, whisperClient, f, contactStore, evolutionClient, ollamaClient, stateStore, cfg.OllamaConcurrency)
	if err != nil {
		slog.Error("failed to create telegram bot", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	go telegramBot.Start(ctx)
	slog.Info("telegram bot started")

	handler := api.NewHandler(whisperClient, telegramBot, evolutionClient, ollamaClient, f, cfg.OwnerPhone)

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}

	go func() {
		slog.Info("http server starting", "addr", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	server.Close()
}
