package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/barrymac/whisper-watch/internal/bot"
	"github.com/barrymac/whisper-watch/internal/evolution"
	"github.com/barrymac/whisper-watch/internal/filters"
	"github.com/barrymac/whisper-watch/internal/metrics"
	"github.com/barrymac/whisper-watch/internal/ollama"
	"github.com/barrymac/whisper-watch/internal/state"
	"github.com/barrymac/whisper-watch/internal/whisper"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	maxConcurrentMessages = 3
	transcribeMaxRetries  = 3
)

var defaultTranscribeRetryDelay = 5 * time.Second

type Handler struct {
	whisper    *whisper.Client
	bot        *bot.TelegramBot
	evolution  *evolution.Client
	ollama     *ollama.Client
	filters    *filters.Filters
	state      *state.Store
	ownerPhone string
	mux        *http.ServeMux

	sem             chan struct{}
	seenMu          sync.Mutex
	seenIDs         map[string]struct{}
	transcribeDelay time.Duration
}

type translateResponse struct {
	Filename string `json:"filename"`
	Text     string `json:"text"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHandler(whisperClient *whisper.Client, telegramBot *bot.TelegramBot, evolutionClient *evolution.Client, ollamaClient *ollama.Client, f *filters.Filters, stateStore *state.Store, ownerPhone string) *Handler {
	sem := make(chan struct{}, maxConcurrentMessages)
	for i := 0; i < maxConcurrentMessages; i++ {
		sem <- struct{}{}
	}
	h := &Handler{
		whisper:         whisperClient,
		bot:             telegramBot,
		evolution:       evolutionClient,
		ollama:          ollamaClient,
		filters:         f,
		state:           stateStore,
		ownerPhone:      ownerPhone,
		mux:             http.NewServeMux(),
		sem:             sem,
		seenIDs:         make(map[string]struct{}),
		transcribeDelay: defaultTranscribeRetryDelay,
	}

	h.mux.HandleFunc("POST /v1/translate", h.handleTranslate)
	h.mux.HandleFunc("POST /webhook/evolution", h.handleEvolutionWebhook)
	h.mux.HandleFunc("GET /healthz", h.handleLiveness)
	h.mux.HandleFunc("GET /readyz", h.handleReadiness)
	h.mux.Handle("GET /metrics", promhttp.Handler())

	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) translateAudio(filename string, audio *bytes.Reader) (string, error) {
	var lastErr error
	for attempt := 0; attempt < transcribeMaxRetries; attempt++ {
		if attempt > 0 {
			slog.Warn("retrying transcription after error", "attempt", attempt, "error", lastErr)
			time.Sleep(h.transcribeDelay)
			audio.Seek(0, 0)
		}
		if h.ollama != nil {
			raw, err := h.whisper.Transcribe(filename, audio)
			if err != nil {
				lastErr = fmt.Errorf("transcription: %w", err)
				continue
			}
			slog.Info("whisper transcription complete", "raw", raw)
			translated, err := h.ollama.FixAndTranslate(raw)
			if err != nil {
				slog.Warn("ollama failed, falling back to whisper translation", "error", err)
				audio.Seek(0, 0)
				result, err2 := h.whisper.Translate(filename, audio)
				if err2 != nil {
					lastErr = fmt.Errorf("fallback translation: %w", err2)
					continue
				}
				return result, nil
			}
			return translated, nil
		}
		result, err := h.whisper.Translate(filename, audio)
		if err != nil {
			lastErr = fmt.Errorf("translation: %w", err)
			continue
		}
		return result, nil
	}
	return "", lastErr
}

func (h *Handler) handleTranslate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid multipart form"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "missing 'file' field"})
		return
	}
	defer file.Close()

	slog.Info("received translation request", "filename", header.Filename, "size", header.Size)

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(file); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "reading file"})
		return
	}

	text, err := h.translateAudio(header.Filename, bytes.NewReader(buf.Bytes()))
	if err != nil {
		slog.Error("translation failed", "filename", header.Filename, "error", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: fmt.Sprintf("translation failed: %v", err)})
		return
	}

	slog.Info("translation complete", "filename", header.Filename, "length", len(text))

	if h.bot != nil {
		if err := h.bot.SendTranslation(context.Background(), header.Filename, text); err != nil {
			slog.Error("failed to send telegram notification", "error", err)
		}
	}

	writeJSON(w, http.StatusOK, translateResponse{
		Filename: header.Filename,
		Text:     text,
	})
}

func (h *Handler) handleEvolutionWebhook(w http.ResponseWriter, r *http.Request) {
	var payload evolution.WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON"})
		return
	}

	switch payload.Event {
	case "labels.association":
		if h.state != nil {
			var ev evolution.LabelAssociationEvent
			if err := json.Unmarshal(payload.Data, &ev); err == nil && ev.Contact != "" {
				if err := h.state.SetContactLabel(ev.Contact, ev.Label.ID, ev.Label.Name); err != nil {
					slog.Warn("failed to store label association", "jid", ev.Contact, "labelId", ev.Label.ID, "error", err)
				} else {
					slog.Info("label associated", "jid", ev.Contact, "labelId", ev.Label.ID, "labelName", ev.Label.Name)
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		return
	case "messages.upsert":
	default:
		w.WriteHeader(http.StatusOK)
		return
	}

	msg, err := evolution.ParseMessageData(payload.Data)
	if err != nil {
		slog.Error("failed to parse evolution message", "error", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	if msg.Key.FromMe {
		w.WriteHeader(http.StatusOK)
		return
	}

	if h.filters.IsMuted(msg.Key.RemoteJid) {
		slog.Info("muted message", "jid", msg.Key.RemoteJid, "name", msg.PushName)
		metrics.WebhookMessages.WithLabelValues("muted").Inc()
		w.WriteHeader(http.StatusOK)
		return
	}

	if msg.Key.ID != "" {
		h.seenMu.Lock()
		_, already := h.seenIDs[msg.Key.ID]
		if !already {
			h.seenIDs[msg.Key.ID] = struct{}{}
		}
		h.seenMu.Unlock()
		if already {
			slog.Info("duplicate message ignored", "id", msg.Key.ID)
			metrics.WebhookMessages.WithLabelValues("duplicate").Inc()
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	slog.Info("evolution webhook received",
		"from", msg.Key.RemoteJid,
		"name", msg.PushName,
		"type", msg.MessageType,
	)

	msgType := "text"
	if msg.IsAudio() {
		msgType = "audio"
	}
	metrics.WebhookMessages.WithLabelValues(msgType).Inc()
	go func() {
		<-h.sem
		defer func() { h.sem <- struct{}{} }()
		h.processEvolutionMessage(msg)
	}()
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) processEvolutionMessage(msg *evolution.MessageData) {
	if h.evolution == nil || h.ownerPhone == "" {
		slog.Warn("evolution client not configured, skipping message")
		return
	}
	start := time.Now()
	defer func() { metrics.WebhookProcessDuration.Observe(time.Since(start).Seconds()) }()

	contact := msg.PushName
	if contact == "" {
		contact = strings.TrimSuffix(msg.Key.RemoteJid, "@s.whatsapp.net")
	}

	var translatedText string

	if msg.IsAudio() {
		if !h.filters.TranslateAudio() {
			slog.Info("audio translation disabled, skipping", "from", contact)
			return
		}
		slog.Info("downloading audio via Evolution API", "from", contact)
		dlStart := time.Now()
		audioData, _, err := h.evolution.DownloadMediaByMessage(msg.Raw)
		metrics.EvolutionDownloadDuration.Observe(time.Since(dlStart).Seconds())
		if err != nil {
			slog.Error("failed to download audio", "error", err)
			return
		}

		text, err := h.translateAudio("audio.ogg", bytes.NewReader(audioData))
		if err != nil {
			slog.Error("audio translation failed", "error", err)
			return
		}
		translatedText = text
		slog.Info("audio translated", "from", contact, "length", len(text))

	} else {
		if !h.filters.TranslateText() {
			slog.Info("text translation disabled, skipping", "from", contact)
			metrics.WebhookMessages.WithLabelValues("skipped_disabled").Inc()
			return
		}
		raw := msg.TextContent()
		if raw == "" {
			return
		}
		if h.ollama != nil {
			translated, isEnglish, err := h.ollama.TranslateText(raw)
			if err != nil {
				slog.Warn("ollama text translation failed, forwarding original", "error", err)
				translatedText = raw
			} else if isEnglish {
				slog.Info("text message already in English, skipping", "from", contact)
				return
			} else {
				translatedText = translated
			}
		} else {
			translatedText = raw
		}
		slog.Info("text message translated", "from", contact)
	}

	selfMessage := fmt.Sprintf("📞 *%s:*\n\n%s", contact, translatedText)
	if err := h.evolution.SendText(h.ownerPhone, selfMessage); err != nil {
		slog.Error("failed to send self-message", "error", err)
	} else {
		slog.Info("self-message sent", "to", h.ownerPhone, "contact", contact)
	}

	if h.ollama != nil && h.filters.ReplyDrafts() {
		draft, err := h.ollama.DraftReply(translatedText)
		if err != nil {
			slog.Warn("reply draft failed", "error", err)
			return
		}
		draftMessage := fmt.Sprintf("💬 *Draft reply to %s:*\n\n%s", contact, draft)
		if err := h.evolution.SendText(h.ownerPhone, draftMessage); err != nil {
			slog.Error("failed to send draft reply", "error", err)
		} else {
			slog.Info("draft reply sent", "contact", contact)
		}
	}
}

func (h *Handler) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (h *Handler) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	if !h.whisper.Healthy() {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "speaches backend unhealthy"})
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
