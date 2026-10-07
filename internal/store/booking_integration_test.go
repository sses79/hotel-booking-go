package store

import (
	"context"
	"errors"
	"github.com/sses79/hotel-booking-go/internal/booking"
	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/dbschema"
	"github.com/sses79/hotel-booking-go/internal/events"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBookingOracleIntegration(t *testing.T) {
	if os.Getenv("ORACLE_INTEGRATION") != "1" {
		t.Skip("requires local test schema")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env != "local" || !strings.EqualFold(cfg.OracleUser, "hotel_booking_test") {
		t.Fatal("requires local hotel_booking_test schema")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := dbschema.Apply(ctx, s.db, "../../schema"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Reset(context.Background())
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(table string, want int) {
		t.Helper()
		var got int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s=%d want=%d", table, got, want)
		}
	}
	in := time.Now().UTC().AddDate(0, 0, 10)
	stay := booking.Stay{HotelID: DemoHotelID, CheckIn: in.Format(time.DateOnly), CheckOut: in.AddDate(0, 0, 2).Format(time.DateOnly), Guests: 2}
	rooms, err := s.Available(ctx, stay)
	if err != nil || len(rooms) != 4 || rooms[0].Number != "201" || rooms[2].Number != "301" {
		t.Fatalf("rooms=%+v err=%v", rooms, err)
	}
	unknown := stay
	unknown.HotelID = "missing"
	if _, err := s.Available(ctx, unknown); !errors.Is(err, booking.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Lookup(ctx, "missing"); !errors.Is(err, booking.ErrNotFound) {
		t.Fatal(err)
	}
	req := booking.Request{Stay: stay, GuestName: "Fictional Guest"}
	d, e, err := s.Book(ctx, req)
	if err != nil || d.RoomNumber != "201" || len(d.Reference) != 35 || d.Delivery != "pending" {
		t.Fatalf("details=%+v err=%v", d, err)
	}
	look, err := s.Lookup(ctx, d.Reference)
	if err != nil || look.RoomID != d.RoomID || look.HotelName != DemoHotelName || look.GuestCount != 2 || look.CheckIn != stay.CheckIn {
		t.Fatalf("lookup=%+v %v", look, err)
	}
	count("Bookings", 1)
	count("OccupiedNights", 2)
	count("OutboxEvents", 1)
	rooms, err = s.Available(ctx, stay)
	if err != nil || len(rooms) != 3 || rooms[0].Number != "202" {
		t.Fatalf("available=%+v err=%v", rooms, err)
	}
	back := stay
	back.CheckIn = stay.CheckOut
	back.CheckOut = in.AddDate(0, 0, 3).Format(time.DateOnly)
	rooms, err = s.Available(ctx, back)
	if err != nil || rooms[0].Number != "201" {
		t.Fatalf("back-to-back=%+v err=%v", rooms, err)
	}
	for range 2 {
		if err := s.Process(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	count("ProcessedEvents", 1)
	// Concurrent duplicate worker deliveries also produce one effect.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Process(ctx, e) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	count("ProcessedEvents", 1)
	if err := s.MarkPublished(ctx, e.EventID); err != nil {
		t.Fatal(err)
	}
	look, err = s.Lookup(ctx, d.Reference)
	if err != nil || look.Delivery != "published" {
		t.Fatal(look, err)
	}
	pending, err := s.Pending(ctx, 100)
	if err != nil || len(pending) != 0 {
		t.Fatal(pending, err)
	}

	t.Run("late outbox failure rolls back booking and nights", func(t *testing.T) {
		exec(`CREATE OR REPLACE TRIGGER FailOutboxInsert BEFORE INSERT ON OutboxEvents BEGIN RAISE_APPLICATION_ERROR(-20003, 'injected outbox failure'); END;`)
		defer exec("DROP TRIGGER FailOutboxInsert")
		if _, _, err := s.Book(ctx, req); err == nil {
			t.Fatal("expected failure")
		}
		count("Bookings", 1)
		count("OccupiedNights", 2)
		count("OutboxEvents", 1)
	})
	// One deluxe room is already occupied; two contenders race for the last.
	req.Guests = 4
	req.RoomType = "deluxe"
	if _, _, err := s.Book(ctx, req); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, _, err := s.Book(ctx, req); results <- err }()
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, booking.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	count("Bookings", 3)
	count("OccupiedNights", 6)
	count("OutboxEvents", 3)
	// Force a candidate collision after an earlier night insert to prove rollback
	// removes that earlier night as well, rather than leaving a partial stay.
	room := booking.Room{ID: demoRooms[2].id, HotelID: DemoHotelID, Number: "201", Type: "double", Capacity: 2}
	partial := booking.Request{Stay: stay, GuestName: "Race Guest"}
	partial.CheckIn = in.AddDate(0, 0, -1).Format(time.DateOnly)
	_, _, err = s.bookCandidate(ctx, partial, room)
	if !uniqueViolation(err) {
		t.Fatalf("expected night constraint, got %v", err)
	}
	count("Bookings", 3)
	count("OccupiedNights", 6)
	count("OutboxEvents", 3)
	// Failed publication does not lose a committed booking; the same ID replays.
	p := &integrationPublisher{fail: true, store: s}
	if err := events.Deliver(ctx, s, p, e); err == nil {
		t.Fatal("expected publish failure")
	}
	p.fail = false
	if n, err := events.Replay(ctx, s, p, 100); err != nil || n != 2 {
		t.Fatalf("replay n=%d err=%v", n, err)
	}
	if n, err := events.Replay(ctx, s, p, 100); err != nil || n != 0 {
		t.Fatalf("replayed sent events: %d %v", n, err)
	}
	count("ProcessedEvents", 3)
	if _, err := s.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	count("Bookings", 0)
	count("OccupiedNights", 0)
	count("OutboxEvents", 0)
	count("ProcessedEvents", 0)
	if err := s.Process(ctx, e); err != nil {
		t.Fatal(err)
	}
	count("ProcessedEvents", 0) // Redelivery after seed/reset must stay discarded.
}

type integrationPublisher struct {
	fail  bool
	store *Store
}

func (p *integrationPublisher) Publish(ctx context.Context, b []byte) error {
	if p.fail {
		return errors.New("injected publisher outage")
	}
	e, err := events.Decode(b)
	if err != nil {
		return err
	}
	for range 2 {
		if err := p.store.Process(ctx, e); err != nil {
			return err
		}
	}
	return nil
}
