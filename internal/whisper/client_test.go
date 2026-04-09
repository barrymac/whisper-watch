package whisper

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthy_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tiny", "pt")
	if !c.Healthy() {
		t.Error("expected Healthy() = true when server returns 200")
	}
}

func TestHealthy_Down(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tiny", "pt")
	if c.Healthy() {
		t.Error("expected Healthy() = false when server returns 503")
	}
}

func TestHealthy_Unreachable(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "tiny", "pt")
	if c.Healthy() {
		t.Error("expected Healthy() = false when server is unreachable")
	}
}
