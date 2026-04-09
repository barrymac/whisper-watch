package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

func webhookBody(msg evolution.MessageData) string {
	msgJSON, _ := json.Marshal(msg)
	payload := evolution.WebhookPayload{Event: "messages.upsert", Data: msgJSON}
	body, _ := json.Marshal(payload)
	return string(body)
}

type stubTranscriber struct {
	transcribeFunc func(string, io.Reader) (string, error)
	translateFunc  func(string, io.Reader) (string, error)
	healthy        bool
}

func (s *stubTranscriber) Transcribe(filename string, audio io.Reader) (string, error) {
	if s.transcribeFunc != nil {
		return s.transcribeFunc(filename, audio)
	}
	return "", fmt.Errorf("stub: not configured")
}
func (s *stubTranscriber) Translate(filename string, audio io.Reader) (string, error) {
	if s.translateFunc != nil {
		return s.translateFunc(filename, audio)
	}
	return "translated text", nil
}
func (s *stubTranscriber) Healthy() bool { return s.healthy }

type stubEvolution struct {
	downloadFunc func(json.RawMessage) ([]byte, string, error)
	sent         []string
}

func (s *stubEvolution) DownloadMediaByMessage(raw json.RawMessage) ([]byte, string, error) {
	if s.downloadFunc != nil {
		return s.downloadFunc(raw)
	}
	return []byte("audio"), "audio/ogg", nil
}
func (s *stubEvolution) SendText(to, text string) error {
	s.sent = append(s.sent, text)
	return nil
}

type stubOllama struct {
	translateTextFunc func(string) (string, bool, error)
}

func (s *stubOllama) FixAndTranslate(text string) (string, error) { return text, nil }
func (s *stubOllama) TranslateText(text string) (string, bool, error) {
	if s.translateTextFunc != nil {
		return s.translateTextFunc(text)
	}
	return "translated: " + text, false, nil
}
func (s *stubOllama) DraftReply(text string) (string, error) { return "draft", nil }

type stubLabelStorer struct {
	stored []string
}

func (s *stubLabelStorer) SetContactLabel(jid, labelID, labelName string) error {
	s.stored = append(s.stored, jid+":"+labelID)
	return nil
}

func newStubHandler(tc transcribeClient, ev evolutionMessenger, ol ollamaTranslator, ls labelStorer, f *filters.Filters, owner string) *Handler {
	h := newHandler(tc, nil, ev, ol, f, ls, owner)
	h.transcribeDelay = 0
	return h
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
	if h.seen.len() != 1 {
		t.Fatalf("expected 1 entry in seen set after first delivery, got %d", h.seen.len())
	}
	if !h.seen.seen(msg.Key.ID) {
		t.Fatal("message ID should be marked seen after first delivery")
	}

	if rec := send(); rec.Code != http.StatusOK {
		t.Fatalf("second send: expected 200, got %d", rec.Code)
	}
	if h.seen.len() != 1 {
		t.Errorf("expected 1 entry in seen set after duplicate delivery, got %d", h.seen.len())
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

	w := whisper.NewClient(fakeSpeaches.URL, "tiny", "pt")
	h := newStubHandler(w, nil, nil, nil, filters.New(false, map[string]bool{}), "")
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

	w := whisper.NewClient(fakeSpeaches.URL, "tiny", "pt")
	h := newStubHandler(w, nil, nil, nil, filters.New(false, map[string]bool{}), "")
	_, err := h.translateAudio("test.ogg", bytes.NewReader([]byte("fake audio")))
	if err == nil {
		t.Fatal("expected error after exhausting all retries")
	}
	if calls.Load() != transcribeMaxRetries {
		t.Errorf("expected %d speaches calls, got %d", transcribeMaxRetries, calls.Load())
	}
}

func TestReadiness_Healthy(t *testing.T) {
	tc := &stubTranscriber{healthy: true}
	h := newStubHandler(tc, nil, nil, nil, filters.New(false, map[string]bool{}), "")
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestReadiness_Unhealthy(t *testing.T) {
	tc := &stubTranscriber{healthy: false}
	h := newStubHandler(tc, nil, nil, nil, filters.New(false, map[string]bool{}), "")
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestEvolutionWebhook_LabelAssociation(t *testing.T) {
	ls := &stubLabelStorer{}
	h := newStubHandler(&stubTranscriber{}, nil, nil, ls, filters.New(false, map[string]bool{}), "")

	ev := evolution.LabelAssociationEvent{
		Contact: "557192669940@s.whatsapp.net",
		Label:   evolution.LabelAssociationData{ID: "3", Name: "business"},
	}
	evJSON, _ := json.Marshal(ev)
	payload := evolution.WebhookPayload{Event: "labels.association", Data: evJSON}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhook/evolution", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if len(ls.stored) != 1 || ls.stored[0] != "557192669940@s.whatsapp.net:3" {
		t.Errorf("expected label stored, got %v", ls.stored)
	}
}

func TestProcessMessage_TextNoOllama(t *testing.T) {
	ev := &stubEvolution{}
	h := newStubHandler(&stubTranscriber{}, ev, nil, nil, filters.New(false, map[string]bool{}), "owner@s.whatsapp.net")
	done := make(chan struct{}, 1)
	h.afterProcess = func() { done <- struct{}{} }

	msg := evolution.MessageData{}
	msg.Key.RemoteJid = "557192669940@s.whatsapp.net"
	msg.Key.ID = "unique-text-1"
	msg.PushName = "Alice"
	msg.MessageType = "conversation"
	raw, _ := json.Marshal(map[string]string{"conversation": "Olá tudo bem?"})
	msg.Message = raw

	hrec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook/evolution", strings.NewReader(webhookBody(msg)))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(hrec, req)
	if hrec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", hrec.Code)
	}
	<-done

	if len(ev.sent) == 0 {
		t.Error("expected self-message to be sent when ollama not configured")
	}
	if !strings.Contains(ev.sent[0], "Alice") {
		t.Errorf("expected contact name in message, got: %s", ev.sent[0])
	}
}

func TestProcessMessage_TextWithOllama(t *testing.T) {
	ev := &stubEvolution{}
	ol := &stubOllama{
		translateTextFunc: func(text string) (string, bool, error) {
			return "Hello all good?", false, nil
		},
	}
	h := newStubHandler(&stubTranscriber{}, ev, ol, nil, filters.New(false, map[string]bool{}), "owner@s.whatsapp.net")
	done := make(chan struct{}, 1)
	h.afterProcess = func() { done <- struct{}{} }

	msg := evolution.MessageData{}
	msg.Key.RemoteJid = "557192669940@s.whatsapp.net"
	msg.Key.ID = "unique-text-2"
	msg.PushName = "Bob"
	msg.MessageType = "conversation"
	raw, _ := json.Marshal(map[string]string{"conversation": "Olá tudo bem?"})
	msg.Message = raw

	req := httptest.NewRequest(http.MethodPost, "/webhook/evolution", strings.NewReader(webhookBody(msg)))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), req)
	<-done

	if len(ev.sent) == 0 {
		t.Fatal("expected self-message to be sent")
	}
	if !strings.Contains(ev.sent[0], "Hello all good?") {
		t.Errorf("expected ollama translation in message, got: %s", ev.sent[0])
	}
}

func TestTranslateAudio_NoOllama(t *testing.T) {
	tc := &stubTranscriber{
		translateFunc: func(filename string, audio io.Reader) (string, error) {
			return "whisper translated", nil
		},
		healthy: true,
	}
	h := newStubHandler(tc, nil, nil, nil, filters.New(false, map[string]bool{}), "")

	result, err := h.translateAudio("test.ogg", bytes.NewReader([]byte("audio")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "whisper translated" {
		t.Errorf("expected 'whisper translated', got %q", result)
	}
}

func TestEvolutionWebhook_NoMsgIDSkipsDedup(t *testing.T) {
	h := newTestHandler(filters.New(false, map[string]bool{}))

	msg := evolution.MessageData{}
	msg.Key.ID = ""
	msg.Key.RemoteJid = "557192669940@s.whatsapp.net"
	msg.Key.FromMe = false
	msg.MessageType = "conversation"

	send := func() int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/webhook/evolution",
			strings.NewReader(webhookBody(msg)))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := send(); code != http.StatusOK {
		t.Fatalf("first: expected 200, got %d", code)
	}
	if code := send(); code != http.StatusOK {
		t.Fatalf("second: expected 200, got %d", code)
	}
	if h.seen.len() != 0 {
		t.Errorf("empty message ID should not be tracked in seenSet, got len=%d", h.seen.len())
	}
}
