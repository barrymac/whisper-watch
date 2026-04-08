package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	WhisperDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ww_whisper_duration_seconds",
		Help:    "Latency of speaches (Whisper) calls",
		Buckets: []float64{1, 2, 5, 10, 20, 40, 60, 120},
	}, []string{"op"})

	OllamaDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ww_ollama_duration_seconds",
		Help:    "Latency of ollama LLM calls",
		Buckets: []float64{0.5, 1, 2, 5, 10, 20, 40, 60},
	}, []string{"op"})

	OllamaErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ww_ollama_errors_total",
		Help: "Total ollama call errors",
	}, []string{"op"})

	WebhookMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ww_webhook_messages_total",
		Help: "WhatsApp webhook messages received, by disposition",
	}, []string{"type"})

	WebhookProcessDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "ww_webhook_process_duration_seconds",
		Help:    "End-to-end processing time per WhatsApp message (download + transcribe + translate + send)",
		Buckets: []float64{1, 2, 5, 10, 20, 40, 60, 120},
	})

	EvolutionDownloadDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "ww_evolution_download_duration_seconds",
		Help:    "Latency of media download from Evolution API",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
	})

	BootstrapClassified = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ww_bootstrap_classified_total",
		Help: "Contact classifications during /bootstrap, by result",
	}, []string{"result"})
)
