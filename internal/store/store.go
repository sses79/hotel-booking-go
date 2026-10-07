package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	go_ora "github.com/sijms/go-ora/v2"
	"github.com/sses79/hotel-booking-go/internal/config"
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

type Store struct{ db *sql.DB }

func New(ctx context.Context, cfg config.Config) (*Store, error) {
	db, err := Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Open creates a bounded pool. Callers own the returned pool and must close it.
func Open(ctx context.Context, cfg config.Config) (*sql.DB, error) {
	dsn := go_ora.BuildUrl(cfg.OracleHost, cfg.OraclePort, cfg.OracleService,
		cfg.OracleUser, cfg.OraclePassword, map[string]string{
			"CONNECTION TIMEOUT": "5",
			"TIMEOUT":            "30",
		})
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return nil, fmt.Errorf("open Oracle: %w", redactConnectionError(err, dsn, cfg.OraclePassword))
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to Oracle: %w", redactConnectionError(err, dsn, cfg.OraclePassword))
	}
	return db, nil
}

// Driver URL parsing errors can include the input URL. Scrub connection secrets
// before returning errors to handlers that log them.
func redactConnectionError(err error, dsn, password string) error {
	message := strings.ReplaceAll(err.Error(), dsn, "[redacted connection URL]")
	if password != "" {
		for _, secret := range []string{url.QueryEscape(password), url.PathEscape(password), password} {
			message = strings.ReplaceAll(message, secret, "[redacted password]")
		}
	}
	return errors.New(message)
}

func (s *Store) Close() { _ = s.db.Close() }

func (s *Store) Ready(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT 1
        FROM Hotels, Rooms, Bookings, OccupiedNights, OutboxEvents, ProcessedEvents
        WHERE 1 = 0`)
	if err != nil {
		return err
	}
	defer rows.Close()
	return rows.Err()
}

func (s *Store) SearchHotels(ctx context.Context, name string) ([]Hotel, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT HotelId, Name, CreatedAt FROM Hotels ORDER BY Name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	needle := strings.ToLower(strings.TrimSpace(name))
	hotels := make([]Hotel, 0)
	for rows.Next() {
		var h Hotel
		if err := rows.Scan(&h.ID, &h.Name, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.CreatedAt = h.CreatedAt.UTC()
		if needle == "" || strings.Contains(strings.ToLower(h.Name), needle) {
			hotels = append(hotels, h)
		}
	}
	return hotels, rows.Err()
}

func deleteAll(ctx context.Context, tx *sql.Tx) error {
	// Lock/remove outbox rows before audits, matching the consumer's lock order.
	// A delivery already running finishes before its audit is deleted; a late
	// delivery sees no outbox row and cannot recreate data after reset.
	for _, table := range []string{"OutboxEvents", "ProcessedEvents", "OccupiedNights", "Bookings", "Rooms", "Hotels"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("delete %s: %w", table, err)
		}
	}
	return nil
}

func (s *Store) Reset(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deleteAll(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Seed(ctx context.Context) (SeedResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SeedResult{}, err
	}
	defer tx.Rollback()
	if err := deleteAll(ctx, tx); err != nil {
		return SeedResult{}, err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO Hotels (HotelId, Name, CreatedAt) VALUES (:1, :2, :3)",
		DemoHotelID, DemoHotelName, time.Now().UTC()); err != nil {
		return SeedResult{}, err
	}
	for _, room := range demoRooms {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO Rooms (RoomId, HotelId, RoomNumber, RoomType, Capacity) VALUES (:1, :2, :3, :4, :5)",
			room.id, DemoHotelID, room.number, room.roomType, room.capacity); err != nil {
			return SeedResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SeedResult{}, err
	}
	return SeedResult{HotelID: DemoHotelID, HotelName: DemoHotelName, RoomsCreated: len(demoRooms)}, nil
}
