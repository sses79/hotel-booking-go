package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/dbschema"
)

// This suite owns only the explicitly named local test schema. Never point it
// at the demo/application user: it drops tables to verify first-time setup.
func TestOracleIntegration(t *testing.T) {
	if os.Getenv("ORACLE_INTEGRATION") != "1" {
		t.Skip("run the Compose test service to enable Oracle integration tests")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env != "local" || !strings.EqualFold(cfg.OracleUser, "hotel_booking_test") {
		t.Fatal("integration tests require APP_ENV=local and ORACLE_USER=hotel_booking_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tables := []string{"ProcessedEvents", "OutboxEvents", "OccupiedNights", "Bookings", "Rooms", "Hotels"}
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	counts := func(t *testing.T, want int) {
		t.Helper()
		for _, table := range tables {
			var count int
			if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != want {
				t.Fatalf("%s count=%d; want %d", table, count, want)
			}
		}
	}
	for _, table := range tables {
		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = :1", strings.ToUpper(table)).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			exec(t, "DROP TABLE "+table+" CASCADE CONSTRAINTS PURGE")
		}
	}
	if err := s.Ready(ctx); err == nil {
		t.Fatal("database without application tables must not be ready")
	}
	apply := func(t *testing.T) {
		t.Helper()
		if err := dbschema.Apply(ctx, s.db, "../../schema"); err != nil {
			t.Fatal(err)
		}
	}
	apply(t)
	apply(t)
	if err := s.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	counts(t, 0)

	t.Run("schema compatibility and partial setup", func(t *testing.T) {
		exec(t, "ALTER TABLE Hotels MODIFY Name VARCHAR2(201 CHAR)")
		if err := dbschema.Apply(ctx, s.db, "../../schema"); err == nil {
			t.Fatal("incompatible column length was accepted")
		}
		exec(t, "ALTER TABLE Hotels MODIFY Name VARCHAR2(200 CHAR)")
		exec(t, "DROP INDEX HotelsByName")
		exec(t, "CREATE INDEX HotelsByName ON Hotels(Name)")
		if err := dbschema.Apply(ctx, s.db, "../../schema"); err == nil {
			t.Fatal("non-unique index was accepted")
		}
		exec(t, "DROP INDEX HotelsByName")
		exec(t, "DROP TABLE ProcessedEvents PURGE")
		if err := s.Ready(ctx); err == nil {
			t.Fatal("partially initialized schema must not be ready")
		}
		apply(t)
	})

	t.Run("seed and hotel search", func(t *testing.T) {
		for range 2 {
			result, err := s.Seed(ctx)
			if err != nil || result.HotelID != DemoHotelID || result.HotelName != DemoHotelName || result.RoomsCreated != 6 {
				t.Fatalf("seed=%+v err=%v", result, err)
			}
		}
		rows, err := s.db.QueryContext(ctx, "SELECT RoomId, RoomNumber, RoomType, Capacity FROM Rooms ORDER BY RoomNumber")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		i := 0
		for rows.Next() {
			var room roomSeed
			if err := rows.Scan(&room.id, &room.number, &room.roomType, &room.capacity); err != nil {
				t.Fatal(err)
			}
			if i >= len(demoRooms) || room != demoRooms[i] {
				t.Fatalf("unexpected room %+v at %d", room, i)
			}
			i++
		}
		if err := rows.Err(); err != nil || i != 6 {
			t.Fatalf("rooms=%d err=%v", i, err)
		}
		rows.Close()
		for _, name := range []string{"", "Grand", "  gRaNd  ", "plaza"} {
			hotels, err := s.SearchHotels(ctx, name)
			if err != nil || len(hotels) != 1 || hotels[0].Name != DemoHotelName || hotels[0].CreatedAt.Location() != time.UTC || time.Since(hotels[0].CreatedAt) < -time.Second || time.Since(hotels[0].CreatedAt) > time.Minute {
				t.Fatalf("search %q: %+v, %v", name, hotels, err)
			}
		}
		for _, name := range []string{"missing", "%", "_", "' OR 1=1 --"} {
			hotels, err := s.SearchHotels(ctx, name)
			if err != nil || hotels == nil || len(hotels) != 0 {
				t.Fatalf("search %q: %+v, %v", name, hotels, err)
			}
		}
		fixedTime := time.Date(2030, 1, 2, 3, 4, 5, 123456000, time.UTC)
		exec(t, "INSERT INTO Hotels VALUES (:1, :2, :3)", "other", "Alpha Hôtel", fixedTime)
		hotels, err := s.SearchHotels(ctx, "")
		if err != nil || len(hotels) != 2 || hotels[0].Name != "Alpha Hôtel" {
			t.Fatalf("ordered search: %+v, %v", hotels, err)
		}
		hotels, err = s.SearchHotels(ctx, "HÔTEL")
		if err != nil || len(hotels) != 1 || hotels[0].ID != "other" || !hotels[0].CreatedAt.Equal(fixedTime) {
			t.Fatalf("unicode search: %+v, %v", hotels, err)
		}
		if _, err := s.Seed(ctx); err != nil {
			t.Fatal(err)
		}
	})

	// Put data in every table so a late failure proves all earlier deletes roll
	// back, including the outbox CLOB and occupied-night DATE bindings.
	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	exec(t, "INSERT INTO Hotels VALUES ('old-hotel', 'Old hotel', :1)", now)
	exec(t, "INSERT INTO Rooms VALUES ('old-room', 'old-hotel', '999', 'single', 1)")
	exec(t, "INSERT INTO Bookings VALUES ('old-booking', 'HB-OLD', 'old-hotel', 'old-room', 'Guest', 1, DATE '2030-01-01', DATE '2030-01-02', :1)", now)
	exec(t, "INSERT INTO OccupiedNights VALUES ('old-room', DATE '2030-01-01', 'old-booking')")
	exec(t, "INSERT INTO OutboxEvents VALUES ('old-event', 'old-booking', :1, :2, NULL)", "{}", now)
	exec(t, "INSERT INTO ProcessedEvents VALUES ('old-event', 'old-booking', :1)", now)
	counts(t, 1)

	t.Run("seed rollback", func(t *testing.T) {
		exec(t, `CREATE OR REPLACE TRIGGER FailRoomInsert BEFORE INSERT ON Rooms
		BEGIN RAISE_APPLICATION_ERROR(-20001, 'injected insert failure'); END;`)
		defer exec(t, "DROP TRIGGER FailRoomInsert")
		if _, err := s.Seed(ctx); err == nil {
			t.Fatal("expected injected seed failure")
		}
		counts(t, 1)
		hotels, err := s.SearchHotels(ctx, "Old hotel")
		if err != nil || len(hotels) != 1 || hotels[0].ID != "old-hotel" {
			t.Fatalf("seed failed to roll back: %+v, %v", hotels, err)
		}
	})
	t.Run("reset rollback", func(t *testing.T) {
		exec(t, `CREATE OR REPLACE TRIGGER FailHotelDelete BEFORE DELETE ON Hotels
		BEGIN RAISE_APPLICATION_ERROR(-20002, 'injected delete failure'); END;`)
		defer exec(t, "DROP TRIGGER FailHotelDelete")
		if err := s.Reset(ctx); err == nil {
			t.Fatal("expected injected reset failure")
		}
		counts(t, 1)
	})
	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	counts(t, 0)
	hotels, err := s.SearchHotels(ctx, "")
	if err != nil || hotels == nil || len(hotels) != 0 {
		t.Fatalf("search after reset: %+v, %v", hotels, err)
	}
	t.Run("password URL encoding", func(t *testing.T) {
		originalPassword := cfg.OraclePassword
		exec(t, `ALTER USER hotel_booking_test IDENTIFIED BY "Url@:/?#& safe"`)
		defer exec(t, `ALTER USER hotel_booking_test IDENTIFIED BY "`+strings.ReplaceAll(originalPassword, `"`, `""`)+`"`)
		cfg.OraclePassword = "Url@:/?#& safe"
		db, err := Open(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
	})
	s.Close()
	if err := s.Ready(ctx); err == nil {
		t.Fatal("closed database must not be ready")
	}
}
