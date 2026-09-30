package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/store"
)

type DataStore interface {
	Ready(context.Context) error
	SearchHotels(context.Context, string) ([]store.Hotel, error)
	Seed(context.Context) (store.SeedResult, error)
	Reset(context.Context) error
}

type Handler struct {
	cfg   config.Config
	store DataStore
	log   *slog.Logger
	mux   *http.ServeMux
}

func New(cfg config.Config, data DataStore, logger *slog.Logger) http.Handler {
	h := &Handler{cfg: cfg, store: data, log: logger, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /health/live", h.live)
	h.mux.HandleFunc("GET /health/ready", h.ready)
	h.mux.HandleFunc("GET /api/v1/hotels", h.hotels)
	h.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.problem(w, r, http.StatusNotFound, "not_found", "Resource not found")
	})
	if cfg.DemoRoutesEnabled() {
		h.mux.HandleFunc("POST /api/v1/admin/seed", h.seed)
		h.mux.HandleFunc("POST /api/v1/admin/reset", h.reset)
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" || len(requestID) > 128 {
		var b [12]byte
		if _, err := rand.Read(b[:]); err == nil {
			requestID = hex.EncodeToString(b[:])
		} else {
			requestID = "unknown"
		}
	}
	w.Header().Set("X-Request-ID", requestID)
	status := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	h.mux.ServeHTTP(status, r)
	h.log.Info("request", "request_id", requestID, "method", r.Method,
		"path", r.URL.Path, "status", status.status, "duration_ms", time.Since(started).Milliseconds())
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (h *Handler) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := h.store.Ready(ctx); err != nil {
		h.log.Error("readiness failed", "error", err)
		h.problem(w, r, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) hotels(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if len(name) > 200 {
		h.problem(w, r, http.StatusBadRequest, "invalid_name", "Hotel name must be at most 200 characters")
		return
	}
	hotels, err := h.store.SearchHotels(r.Context(), name)
	if err != nil {
		h.log.Error("hotel search failed", "error", err)
		h.problem(w, r, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, hotels)
}

func (h *Handler) seed(w http.ResponseWriter, r *http.Request) {
	result, err := h.store.Seed(r.Context())
	if err != nil {
		h.log.Error("seed failed", "error", err)
		h.problem(w, r, http.StatusServiceUnavailable, "database_unavailable", "Could not seed demo data")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) reset(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Reset(r.Context()); err != nil {
		h.log.Error("reset failed", "error", err)
		h.problem(w, r, http.StatusServiceUnavailable, "database_unavailable", "Could not reset demo data")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) problem(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"code": code, "message": message, "request_id": w.Header().Get("X-Request-ID"),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
