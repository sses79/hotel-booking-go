package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/sses79/hotel-booking-go/internal/booking"
	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/events"
	"github.com/sses79/hotel-booking-go/internal/store"
	"os"
	"testing"
	"time"
)

func eventDB(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("EVENTS_INTEGRATION") != "1" {
		t.Skip("enable EVENTS_INTEGRATION for local event containers")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env != "local" || cfg.OracleUser != "hotel_booking" {
		t.Fatal("live event checks require local demo schema hotel_booking")
	}
	db, err := store.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func awaitAudit(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var processed, published int
		if err := db.QueryRow("SELECT COUNT(*) FROM ProcessedEvents WHERE EventId = :1", id).Scan(&processed); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT COUNT(*) FROM OutboxEvents WHERE EventId = :1 AND PublishedAt IS NOT NULL", id).Scan(&published); err != nil {
			t.Fatal(err)
		}
		if processed == 1 && published == 1 {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("event was not published and processed before timeout")
}
func duplicate(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	var payload string
	if err := db.QueryRow("SELECT Payload FROM OutboxEvents WHERE EventId = :1", id).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	p, err := events.NewPubSub(context.Background(), "local-hotel", "booking-created")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := p.Publish(ctx, []byte(payload))
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	// Wait for duplicate delivery through the real bridge and function.
	time.Sleep(3 * time.Second)
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM ProcessedEvents WHERE EventId = :1", id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit=%d err=%v", n, err)
	}
}
func TestLocalEvents(t *testing.T) {
	db := eventDB(t)
	base := apiURL(t)
	mustCall(t, base, "POST", "/api/v1/admin/seed", nil, 200, nil)
	var d booking.Details
	mustCall(t, base, "POST", "/api/v1/bookings", booking.Request{Stay: stay(), GuestName: "Event Demo Guest"}, 201, &d)
	awaitAudit(t, db, d.EventID)
	duplicate(t, db, d.EventID)
	t.Log("emulator → bridge → actual CloudEvent function, duplicate audit check passed")
}
func TestPublishOutageBooking(t *testing.T) {
	if os.Getenv("RECOVERY_STAGE") != "outage" {
		t.Skip("recovery script controls this stage")
	}
	db := eventDB(t)
	base := apiURL(t)
	mustCall(t, base, "POST", "/api/v1/admin/seed", nil, 200, nil)
	var d booking.Details
	mustCall(t, base, "POST", "/api/v1/bookings", booking.Request{Stay: stay(), GuestName: "Recovery Guest"}, 201, &d)
	if d.Delivery != "pending" {
		t.Fatalf("expected pending event: %+v", d)
	}
	var pending, nights, processed int
	if err := db.QueryRow("SELECT COUNT(*) FROM OutboxEvents WHERE PublishedAt IS NULL").Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM OccupiedNights WHERE BookingId = :1", d.ID).Scan(&nights); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM ProcessedEvents").Scan(&processed); err != nil {
		t.Fatal(err)
	}
	if pending != 1 || nights != 3 || processed != 0 {
		t.Fatalf("pending=%d nights=%d processed=%d", pending, nights, processed)
	}
	mustCall(t, base, "GET", "/api/v1/bookings/"+d.Reference, nil, 200, nil)
	t.Log("booking committed while Pub/Sub was unavailable; event remains pending")
}
func TestOutboxRecovery(t *testing.T) {
	if os.Getenv("RECOVERY_STAGE") != "recovered" {
		t.Skip("recovery script controls this stage")
	}
	db := eventDB(t)
	_ = apiURL(t)
	var payload string
	if err := db.QueryRow("SELECT E.Payload FROM OutboxEvents E JOIN Bookings B ON B.BookingId = E.BookingId WHERE B.GuestName = 'Recovery Guest'").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var e events.Created
	if err := json.Unmarshal([]byte(payload), &e); err != nil {
		t.Fatal(err)
	}
	awaitAudit(t, db, e.EventID)
	duplicate(t, db, e.EventID)
	t.Log("scheduled outbox recovery and duplicate delivery check passed")
}
