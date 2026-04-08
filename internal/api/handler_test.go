package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/barrymac/whisper-watch/internal/evolution"
	"github.com/barrymac/whisper-watch/internal/filters"
	"github.com/barrymac/whisper-watch/internal/whisper"
)

func newTestHandler(f *filters.Filters) *Handler {
	w := whisper.NewClient("http://fake-speaches:8000", "tiny", "pt")
	return NewHandler(w, nil, nil, nil, f, nil, "5500000000000")
}

func TestHealthz(t *testing.T) {
	h := newTestHandler(filters.New(false, map[string]bool{}, "qwen2.5:7b"))
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
	f := filters.New(true, map[string]bool{}, "qwen2.5:7b")
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
	f := filters.New(false, muted, "qwen2.5:7b")
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
	f := filters.New(false, map[string]bool{}, "qwen2.5:7b")
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
	f := filters.New(false, map[string]bool{}, "qwen2.5:7b")
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
