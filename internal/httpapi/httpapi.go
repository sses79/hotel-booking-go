package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sses79/hotel-booking-go/api"
	"github.com/sses79/hotel-booking-go/internal/booking"
	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/events"
	"github.com/sses79/hotel-booking-go/internal/store"
)

type DataStore interface {
	Ready(context.Context) error
	SearchHotels(context.Context, string) ([]store.Hotel, error)
	Seed(context.Context) (store.SeedResult, error)
	Reset(context.Context) error
	Available(context.Context, booking.Stay) ([]booking.Room, error)
	Book(context.Context, booking.Request) (booking.Details, events.Created, error)
	Lookup(context.Context, string) (booking.Details, error)
}

type Handler struct {
	cfg       config.Config
	store     DataStore
	log       *slog.Logger
	mux       *http.ServeMux
	now       func() time.Time
	publisher events.Publisher
}

func New(cfg config.Config, data DataStore, logger *slog.Logger) http.Handler {
	return NewWithPublisher(cfg, data, logger, nil)
}

func NewWithPublisher(cfg config.Config, data DataStore, logger *slog.Logger, publisher events.Publisher) http.Handler {
	h := &Handler{cfg: cfg, store: data, log: logger, mux: http.NewServeMux(), now: time.Now, publisher: publisher}
	h.mux.HandleFunc("GET /health/live", h.live)
	h.mux.HandleFunc("GET /health/ready", h.ready)
	h.mux.HandleFunc("GET /api/v1/hotels", h.hotels)
	h.mux.HandleFunc("GET /api/v1/hotels/{hotel_id}/rooms/available", h.available)
	h.mux.HandleFunc("POST /api/v1/bookings", h.book)
	h.mux.HandleFunc("GET /api/v1/bookings/{reference}", h.lookup)
	h.mux.HandleFunc("GET /openapi.yaml", serveSpec)
	h.mux.HandleFunc("GET /docs", serveDocs)
	h.mux.Handle("GET /docs/assets/", http.StripPrefix("/docs/", http.FileServerFS(api.Files)))
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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	h.mux.ServeHTTP(status, r)
	h.log.Info("request", "request_id", requestID, "method", r.Method,
		"path", r.URL.Path, "status", status.status, "duration_ms", time.Since(started).Milliseconds())
}

func (h *Handler) available(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	guests, err := strconv.Atoi(q.Get("guests"))
	stay := booking.Stay{HotelID: r.PathValue("hotel_id"), CheckIn: q.Get("check_in"), CheckOut: q.Get("check_out"), Guests: guests, RoomType: q.Get("room_type")}
	if err != nil || stay.Validate(h.now()) != nil {
		h.domainError(w, r, booking.ErrInvalid)
		return
	}
	rooms, err := h.store.Available(r.Context(), stay)
	if err != nil {
		h.domainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rooms)
}

func (h *Handler) book(w http.ResponseWriter, r *http.Request) {
	var req booking.Request
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		h.domainError(w, r, booking.ErrInvalid)
		return
	}
	if err := d.Decode(new(any)); err != io.EOF || req.Validate(h.now()) != nil {
		h.domainError(w, r, booking.ErrInvalid)
		return
	}
	details, event, err := h.store.Book(r.Context(), req)
	if err != nil {
		h.domainError(w, r, err)
		return
	}
	if h.publisher != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if outbox, ok := h.store.(events.Outbox); ok {
			if err := events.Deliver(ctx, outbox, h.publisher, event); err != nil {
				h.log.Warn("event pending", "event_id", event.EventID, "error", err)
			} else {
				details.Delivery = "published"
			}
		}
	}
	w.Header().Set("Location", "/api/v1/bookings/"+details.Reference)
	writeJSON(w, http.StatusCreated, details)
}

func (h *Handler) lookup(w http.ResponseWriter, r *http.Request) {
	details, err := h.store.Lookup(r.Context(), r.PathValue("reference"))
	if err != nil {
		h.domainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, details)
}

func (h *Handler) domainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, booking.ErrInvalid):
		h.problem(w, r, 400, "invalid_request", "Use a stay of 1–30 nights starting today or later, 1–4 guests, a supported room type, and a guest name of 1–200 characters")
	case errors.Is(err, booking.ErrNotFound):
		h.problem(w, r, 404, "not_found", "Resource not found")
	case errors.Is(err, booking.ErrConflict):
		h.problem(w, r, 409, "unavailable", "No suitable room is available for the whole stay")
	default:
		h.log.Error("database operation failed", "error", err)
		h.problem(w, r, 503, "database_unavailable", "Database is unavailable")
	}
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
	if utf8.RuneCountInString(name) > 200 {
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
