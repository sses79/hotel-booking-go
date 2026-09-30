package hotelbooking

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/httpapi"
	"github.com/sses79/hotel-booking-go/internal/store"
)

var (
	storeMu     sync.Mutex
	sharedStore *store.Store
	logger      = slog.New(slog.NewJSONHandler(os.Stdout, nil))
)

func init() {
	functions.HTTP("HotelAPI", serveAPI)
}

func serveAPI(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		http.Error(w, "invalid configuration", http.StatusInternalServerError)
		return
	}
	httpapi.New(cfg, lazyStore{cfg: cfg}, logger).ServeHTTP(w, r)
}

type lazyStore struct{ cfg config.Config }

func (s lazyStore) Ready(ctx context.Context) error {
	data, err := getStore(ctx, s.cfg)
	if err != nil {
		return err
	}
	return data.Ready(ctx)
}

func (s lazyStore) SearchHotels(ctx context.Context, name string) ([]store.Hotel, error) {
	data, err := getStore(ctx, s.cfg)
	if err != nil {
		return nil, err
	}
	return data.SearchHotels(ctx, name)
}

func (s lazyStore) Seed(ctx context.Context) (store.SeedResult, error) {
	data, err := getStore(ctx, s.cfg)
	if err != nil {
		return store.SeedResult{}, err
	}
	return data.Seed(ctx)
}

func (s lazyStore) Reset(ctx context.Context) error {
	data, err := getStore(ctx, s.cfg)
	if err != nil {
		return err
	}
	return data.Reset(ctx)
}

func getStore(ctx context.Context, cfg config.Config) (*store.Store, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if sharedStore == nil {
		data, err := store.New(ctx, cfg)
		if err != nil {
			return nil, err
		}
		sharedStore = data
	}
	return sharedStore, nil
}
