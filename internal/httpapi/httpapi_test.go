package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/store"
)

type fakeStore struct{ seedCalls int }

func (*fakeStore) Ready(context.Context) error { return nil }
func (*fakeStore) SearchHotels(context.Context, string) ([]store.Hotel, error) {
	return []store.Hotel{}, nil
}
func (f *fakeStore) Seed(context.Context) (store.SeedResult, error) {
	f.seedCalls++
	return store.SeedResult{HotelID: store.DemoHotelID, RoomsCreated: 6}, nil
}
func (*fakeStore) Reset(context.Context) error { return nil }

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
