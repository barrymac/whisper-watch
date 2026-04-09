package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/barrymac/whisper-watch/internal/evolution"
	"github.com/barrymac/whisper-watch/internal/filters"
	"github.com/barrymac/whisper-watch/internal/whisper"
)

func newTestHandler(f *filters.Filters) *Handler {
	w := whisper.NewClient("http://fake-speaches:8000", "tiny", "pt")
	return NewHandler(w, nil, nil, nil, f, nil, "5500000000000")
}

func newTestHandlerWithSpeaches(speachesURL string, f *filters.Filters) *Handler {
	w := whisper.NewClient(speachesURL, "Systran/faster-whisper-large-v3", "pt")
	h := NewHandler(w, nil, nil, nil, f, nil, "5500000000000")
	h.transcribeDelay = 0
	return h
}

func webhookBody(msg evolution.MessageData) string {
	msgJSON, _ := json.Marshal(msg)
	payload := evolution.WebhookPayload{Event: "messages.upsert", Data: msgJSON}
	body, _ := json.Marshal(payload)
	return string(body)
}

func TestHealthz(t *testing.T) {
	h := newTestHandler(filters.New(false, map[string]bool{}))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("expected 'ok', got %q", rec.Body.String())
	}
}

func TestEvolutionWebhook_MutedGroup(t *testing.T) {
	f := filters.New(true, map[string]bool{})
	h := newTestHandler(f)

	msg := evolution.MessageData{}
	msg.Key.RemoteJid = "557192682188-1629234117@g.us"
	msg.Key.FromMe = false
	msg.PushName = "Norma"
	msg.MessageType = "conversation"
	msgJSON, _ := json.Marshal(msg)

	payload := evolution.WebhookPayload{
		Event: "messages.upsert",
		Data:  msgJSON,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhook/evolution", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestEvolutionWebhook_MutedJID(t *testing.T) {
	muted := map[string]bool{"557192669940@s.whatsapp.net": true}
	f := filters.New(false, muted)
	h := newTestHandler(f)

	msg := evolution.MessageData{}
	msg.Key.RemoteJid = "557192669940@s.whatsapp.net"
	msg.Key.FromMe = false
	msg.PushName = "Blocked Person"
	msg.MessageType = "conversation"
	msgJSON, _ := json.Marshal(msg)

	payload := evolution.WebhookPayload{
		Event: "messages.upsert",
		Data:  msgJSON,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhook/evolution", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestEvolutionWebhook_SkipsFromMe(t *testing.T) {
	f := filters.New(false, map[string]bool{})
	h := newTestHandler(f)

	msg := evolution.MessageData{}
	msg.Key.RemoteJid = "557192669940@s.whatsapp.net"
	msg.Key.FromMe = true
	msg.MessageType = "conversation"
	msgJSON, _ := json.Marshal(msg)

	payload := evolution.WebhookPayload{
		Event: "messages.upsert",
		Data:  msgJSON,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhook/evolution", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestEvolutionWebhook_IgnoresNonUpsert(t *testing.T) {
	f := filters.New(false, map[string]bool{})
	h := newTestHandler(f)

	payload := evolution.WebhookPayload{
		Event: "connection.update",
		Data:  json.RawMessage(`{}`),
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhook/evolution", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestEvolutionWebhook_DeduplicatesMessages(t *testing.T) {
	h := newTestHandler(filters.New(false, map[string]bool{}))

	msg := evolution.MessageData{}
	msg.Key.ID = "AABBCCDD11223344"
	msg.Key.RemoteJid = "557192669940@s.whatsapp.net"
	msg.Key.FromMe = false
	msg.MessageType = "conversation"

	send := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/webhook/evolution",
			strings.NewReader(webhookBody(msg)))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := send(); rec.Code != http.StatusOK {
		t.Fatalf("first send: expected 200, got %d", rec.Code)
	}
	h.seenMu.Lock()
	_, seen := h.seenIDs[msg.Key.ID]
	h.seenMu.Unlock()
	if !seen {
		t.Fatal("message ID should be in seenIDs after first delivery")
	}

	if rec := send(); rec.Code != http.StatusOK {
		t.Fatalf("second send: expected 200, got %d", rec.Code)
	}
	h.seenMu.Lock()
	count := len(h.seenIDs)
	h.seenMu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 entry in seenIDs, got %d", count)
	}
}

func TestNewHandler_SemaphoreCapacity(t *testing.T) {
	h := newTestHandler(filters.New(false, map[string]bool{}))
	if cap(h.sem) != maxConcurrentMessages {
		t.Errorf("semaphore capacity: want %d, got %d", maxConcurrentMessages, cap(h.sem))
	}
	if len(h.sem) != maxConcurrentMessages {
		t.Errorf("semaphore should be full at init: want %d tokens, got %d", maxConcurrentMessages, len(h.sem))
	}
}

func TestTranslateAudio_RetriesOnSpeaches500(t *testing.T) {
	var calls atomic.Int32
	fakeSpeaches := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("CUDA OOM"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"hello world"}`))
	}))
	defer fakeSpeaches.Close()

	h := newTestHandlerWithSpeaches(fakeSpeaches.URL, filters.New(false, map[string]bool{}))
	result, err := h.translateAudio("test.ogg", bytes.NewReader([]byte("fake audio")))
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 speaches calls (2 failures + 1 success), got %d", calls.Load())
	}
}

func TestTranslateAudio_ExhaustsRetries(t *testing.T) {
	var calls atomic.Int32
	fakeSpeaches := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("CUDA OOM"))
	}))
	defer fakeSpeaches.Close()

	h := newTestHandlerWithSpeaches(fakeSpeaches.URL, filters.New(false, map[string]bool{}))
	_, err := h.translateAudio("test.ogg", bytes.NewReader([]byte("fake audio")))
	if err == nil {
		t.Fatal("expected error after exhausting all retries")
	}
	if calls.Load() != transcribeMaxRetries {
		t.Errorf("expected %d speaches calls, got %d", transcribeMaxRetries, calls.Load())
	}
}
