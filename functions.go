package hotelbooking

import (
	"context"
	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/sses79/hotel-booking-go/internal/booking"
	"github.com/sses79/hotel-booking-go/internal/eventhandler"
	"github.com/sses79/hotel-booking-go/internal/events"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/httpapi"
	"github.com/sses79/hotel-booking-go/internal/store"
)

var (
	storeMu         sync.Mutex
	sharedStore     *store.Store
	sharedPublisher *events.PubSub
	publisherMu     sync.Mutex
	logger          = slog.New(slog.NewJSONHandler(os.Stdout, nil))
)

func init() {
	functions.HTTP("HotelAPI", serveAPI)
	functions.CloudEvent("BookingCreated", consumeBooking)
}

func serveAPI(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		http.Error(w, "invalid configuration", http.StatusInternalServerError)
		return
	}
	var publisher events.Publisher
	if os.Getenv("PUBSUB_TOPIC") != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		p, err := getPublisher(ctx)
		cancel()
		if err != nil {
			logger.Warn("publisher initialization failed", "error", err)
		} else {
			publisher = p
		}
	}
	httpapi.NewWithPublisher(cfg, lazyStore{cfg: cfg}, logger, publisher).ServeHTTP(w, r)
}

func getPublisher(ctx context.Context) (*events.PubSub, error) {
	publisherMu.Lock()
	defer publisherMu.Unlock()
	if sharedPublisher == nil {
		p, err := events.NewPubSub(ctx, os.Getenv("PUBSUB_PROJECT_ID"), os.Getenv("PUBSUB_TOPIC"))
		if err != nil {
			return nil, err
		}
		sharedPublisher = p
	}
	return sharedPublisher, nil
}

func consumeBooking(ctx context.Context, cloud event.Event) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := getStore(ctx, cfg)
	if err != nil {
		return err
	}
	return eventhandler.Handle(ctx, cloud, s)
}

type lazyStore struct{ cfg config.Config }

func (s lazyStore) Available(ctx context.Context, req booking.Stay) ([]booking.Room, error) {
	d, err := getStore(ctx, s.cfg)
	if err != nil {
		return nil, err
	}
	return d.Available(ctx, req)
}
func (s lazyStore) Book(ctx context.Context, req booking.Request) (booking.Details, events.Created, error) {
	d, err := getStore(ctx, s.cfg)
	if err != nil {
		return booking.Details{}, events.Created{}, err
	}
	return d.Book(ctx, req)
}
func (s lazyStore) Lookup(ctx context.Context, ref string) (booking.Details, error) {
	d, err := getStore(ctx, s.cfg)
	if err != nil {
		return booking.Details{}, err
	}
	return d.Lookup(ctx, ref)
}
func (s lazyStore) Pending(ctx context.Context, limit int) ([]events.Created, error) {
	d, err := getStore(ctx, s.cfg)
	if err != nil {
		return nil, err
	}
	return d.Pending(ctx, limit)
}
func (s lazyStore) MarkPublished(ctx context.Context, id string) error {
	d, err := getStore(ctx, s.cfg)
	if err != nil {
		return err
	}
	return d.MarkPublished(ctx, id)
}

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
