package tests

import (
	"github.com/sses79/hotel-booking-go/internal/booking"
	"os"
	"testing"
)

func TestPersistenceBeforeRestart(t *testing.T) {
	if os.Getenv("PERSISTENCE_STAGE") != "before" {
		t.Skip("persistence script controls this stage")
	}
	base := apiURL(t)
	mustCall(t, base, "POST", "/api/v1/admin/seed", nil, 200, nil)
	mustCall(t, base, "POST", "/api/v1/bookings", booking.Request{Stay: stay(), GuestName: "Persistence Guest"}, 201, nil)
}
func TestPersistenceAfterRestart(t *testing.T) {
	if os.Getenv("PERSISTENCE_STAGE") != "after" {
		t.Skip("persistence script controls this stage")
	}
	base := apiURL(t)
	db := eventDB(t)
	var reference, id string
	if err := db.QueryRow("SELECT Reference, BookingId FROM Bookings WHERE GuestName = 'Persistence Guest'").Scan(&reference, &id); err != nil {
		t.Fatal(err)
	}
	var d booking.Details
	mustCall(t, base, "GET", "/api/v1/bookings/"+reference, nil, 200, &d)
	if d.RoomNumber != "201" || d.GuestName != "Persistence Guest" {
		t.Fatal(d)
	}
	var nights, outbox int
	if err := db.QueryRow("SELECT COUNT(*) FROM OccupiedNights WHERE BookingId = :1", id).Scan(&nights); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM OutboxEvents WHERE BookingId = :1", id).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if nights != 3 || outbox != 1 {
		t.Fatalf("nights=%d outbox=%d", nights, outbox)
	}
	t.Log("booking, nights, and outbox survived Oracle restart")
}
