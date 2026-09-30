package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/sses79/hotel-booking-go/internal/config"
	"google.golang.org/api/iterator"
)

const DemoHotelID = "00000000-0000-0000-0000-000000000001"
const DemoHotelName = "Grand Plaza Hotel"

type Hotel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type SeedResult struct {
	HotelID      string `json:"hotel_id"`
	HotelName    string `json:"hotel_name"`
	RoomsCreated int    `json:"rooms_created"`
}

type roomSeed struct {
	id       string
	number   string
	roomType string
	capacity int64
}

var demoRooms = []roomSeed{
	{"00000000-0000-0000-0000-000000000101", "101", "single", 1},
	{"00000000-0000-0000-0000-000000000102", "102", "single", 1},
	{"00000000-0000-0000-0000-000000000201", "201", "double", 2},
	{"00000000-0000-0000-0000-000000000202", "202", "double", 2},
	{"00000000-0000-0000-0000-000000000301", "301", "deluxe", 4},
	{"00000000-0000-0000-0000-000000000302", "302", "deluxe", 4},
}

type Store struct {
	client *spanner.Client
}

func New(ctx context.Context, cfg config.Config) (*Store, error) {
	client, err := spanner.NewClient(ctx, cfg.DatabasePath())
	if err != nil {
		return nil, fmt.Errorf("open Spanner: %w", err)
	}
	return &Store{client: client}, nil
}

func (s *Store) Close() { s.client.Close() }

func (s *Store) Ready(ctx context.Context) error {
	iter := s.client.Single().Query(ctx, spanner.NewStatement("SELECT HotelId FROM Hotels LIMIT 1"))
	defer iter.Stop()
	_, err := iter.Next()
	if err == iterator.Done {
		return nil
	}
	return err
}

func (s *Store) SearchHotels(ctx context.Context, name string) ([]Hotel, error) {
	iter := s.client.Single().Query(ctx, spanner.NewStatement(
		"SELECT HotelId, Name, CreatedAt FROM Hotels ORDER BY Name",
	))
	defer iter.Stop()
	needle := strings.ToLower(strings.TrimSpace(name))
	hotels := make([]Hotel, 0)
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			return hotels, nil
		}
		if err != nil {
			return nil, err
		}
		var h Hotel
		if err := row.Columns(&h.ID, &h.Name, &h.CreatedAt); err != nil {
			return nil, err
		}
		if needle == "" || strings.Contains(strings.ToLower(h.Name), needle) {
			hotels = append(hotels, h)
		}
	}
}

func deleteAll() []*spanner.Mutation {
	return []*spanner.Mutation{
		spanner.Delete("ProcessedEvents", spanner.AllKeys()),
		spanner.Delete("OutboxEvents", spanner.AllKeys()),
		spanner.Delete("OccupiedNights", spanner.AllKeys()),
		spanner.Delete("Bookings", spanner.AllKeys()),
		spanner.Delete("Rooms", spanner.AllKeys()),
		spanner.Delete("Hotels", spanner.AllKeys()),
	}
}

func (s *Store) Reset(ctx context.Context) error {
	_, err := s.client.Apply(ctx, deleteAll())
	return err
}

func (s *Store) Seed(ctx context.Context) (SeedResult, error) {
	mutations := deleteAll()
	mutations = append(mutations, spanner.Insert("Hotels",
		[]string{"HotelId", "Name", "CreatedAt"},
		[]any{DemoHotelID, DemoHotelName, time.Now().UTC()}))
	for _, room := range demoRooms {
		mutations = append(mutations, spanner.Insert("Rooms",
			[]string{"RoomId", "HotelId", "RoomNumber", "RoomType", "Capacity"},
			[]any{room.id, DemoHotelID, room.number, room.roomType, room.capacity}))
	}
	_, err := s.client.Apply(ctx, mutations)
	if err != nil {
		return SeedResult{}, err
	}
	return SeedResult{HotelID: DemoHotelID, HotelName: DemoHotelName, RoomsCreated: len(demoRooms)}, nil
}
