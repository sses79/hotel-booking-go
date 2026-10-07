package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/store"
)

type fakeStore struct {
	seedCalls int
	err       error
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
