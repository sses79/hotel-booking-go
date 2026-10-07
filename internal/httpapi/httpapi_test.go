package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sses79/hotel-booking-go/internal/booking"
	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/events"
	"github.com/sses79/hotel-booking-go/internal/store"
)

type fakeStore struct {
	seedCalls int
	err       error
	bookCalls int
	marks     int
}

func (f *fakeStore) Ready(context.Context) error { return f.err }
func (f *fakeStore) SearchHotels(context.Context, string) ([]store.Hotel, error) {
	return []store.Hotel{}, f.err
}
func (f *fakeStore) Seed(context.Context) (store.SeedResult, error) {
	f.seedCalls++
	return store.SeedResult{HotelID: store.DemoHotelID, RoomsCreated: 6}, f.err
}
func (f *fakeStore) Reset(context.Context) error { return f.err }
func (f *fakeStore) Available(context.Context, booking.Stay) ([]booking.Room, error) {
	return []booking.Room{}, f.err
}

func (f *fakeStore) Book(context.Context, booking.Request) (booking.Details, events.Created, error) {
	f.bookCalls++
	return booking.Details{Reference: "HB-TEST", Delivery: "pending"}, events.Created{}, f.err
}
func (f *fakeStore) Lookup(context.Context, string) (booking.Details, error) {
	return booking.Details{}, f.err
}
func (f *fakeStore) Pending(context.Context, int) ([]events.Created, error) { return nil, nil }
func (f *fakeStore) MarkPublished(context.Context, string) error            { f.marks++; return nil }

type failedPublisher struct{}

func (failedPublisher) Publish(context.Context, []byte) error { return errors.New("broker offline") }

func TestBookingHTTPValidationAndErrorMapping(t *testing.T) {
	valid := `{"hotel_id":"hotel","check_in":"2030-01-01","check_out":"2030-01-02","guests":2,"guest_name":"Demo Guest"}`
	for _, tc := range []struct {
		body string
		err  error
		want int
	}{
		{valid, nil, 201}, {valid, booking.ErrNotFound, 404}, {valid, booking.ErrConflict, 409}, {valid, errors.New("database down"), 503},
		{`{}`, nil, 400}, {valid + ` {}`, nil, 400}, {strings.Replace(valid, `"guests":2`, `"guests":0`, 1), nil, 400},
		{strings.Replace(valid, `"guest_name":"Demo Guest"`, `"unknown":1,"guest_name":"Demo Guest"`, 1), nil, 400},
	} {
		data := &fakeStore{err: tc.err}
		h := New(config.Config{Env: "local"}, data, slog.New(slog.NewTextHandler(io.Discard, nil))).(*Handler)
		h.now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/bookings", strings.NewReader(tc.body)))
		if w.Code != tc.want {
			t.Fatalf("body=%s got=%d want=%d", tc.body, w.Code, tc.want)
		}
		if tc.want == 400 && data.bookCalls != 0 {
			t.Fatal("invalid input reached store")
		}
		if tc.want == 201 && w.Header().Get("Location") != "/api/v1/bookings/HB-TEST" {
			t.Fatal("missing Location")
		}
		if tc.want != 201 {
			var p map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &p)
			if p["request_id"] == "" || p["code"] == "" {
				t.Fatal(p)
			}
		}
	}
}

func TestFailedPublicationStillReturnsCommittedBooking(t *testing.T) {
	data := &fakeStore{}
	h := NewWithPublisher(config.Config{Env: "local"}, data, slog.New(slog.NewTextHandler(io.Discard, nil)), failedPublisher{}).(*Handler)
	h.now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/bookings", strings.NewReader(`{"hotel_id":"hotel","check_in":"2030-01-01","check_out":"2030-01-02","guests":2,"guest_name":"Guest"}`)))
	var d booking.Details
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if w.Code != 201 || d.Delivery != "pending" || data.marks != 0 {
		t.Fatalf("status=%d details=%+v marks=%d", w.Code, d, data.marks)
	}
}

func TestAdminRoutesRequireExplicitDemoEnvironment(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		env        string
		wantStatus int
		wantCalls  int
	}{
		{"production", http.StatusNotFound, 0},
		{"local", http.StatusOK, 1},
		{"demo", http.StatusOK, 1},
	} {
		t.Run(tc.env, func(t *testing.T) {
			data := &fakeStore{}
			handler := New(config.Config{Env: tc.env}, data, log)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/admin/seed", nil))
			if response.Code != tc.wantStatus || data.seedCalls != tc.wantCalls {
				t.Fatalf("status=%d calls=%d; want status=%d calls=%d",
					response.Code, data.seedCalls, tc.wantStatus, tc.wantCalls)
			}
		})
	}
}

func TestLivenessDoesNotRequireStore(t *testing.T) {
	handler := New(config.Config{Env: "production"}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d; want 200", response.Code)
	}
}

func TestProductionResetRouteIsHidden(t *testing.T) {
	handler := New(config.Config{Env: "production"}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/admin/reset", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d; want 404", response.Code)
	}
}

func TestDatabaseOutagesReturnStableErrors(t *testing.T) {
	for _, route := range []struct{ method, path string }{
		{"GET", "/health/ready"}, {"GET", "/api/v1/hotels"},
		{"POST", "/api/v1/admin/seed"}, {"POST", "/api/v1/admin/reset"},
	} {
		t.Run(route.path, func(t *testing.T) {
			handler := New(config.Config{Env: "local"}, &fakeStore{err: errors.New("database offline")}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(route.method, route.path, nil))
			var problem map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusServiceUnavailable || problem["code"] != "database_unavailable" || problem["message"] == "" || problem["request_id"] != response.Header().Get("X-Request-ID") || problem["request_id"] == "" {
				t.Fatalf("unexpected outage response: %d %v", response.Code, problem)
			}
		})
	}
}
