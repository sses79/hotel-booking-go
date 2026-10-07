package booking

import (
	"testing"
	"time"
)

func TestStayBoundaries(t *testing.T) {
	now := time.Date(2030, 1, 1, 23, 59, 0, 0, time.UTC)
	for _, tc := range []struct {
		in, out string
		guests  int
		kind    string
		valid   bool
	}{
		{"2030-01-01", "2030-01-02", 1, "", true},
		{"2030-01-01", "2030-01-31", 4, "deluxe", true},
		{"2029-12-31", "2030-01-02", 1, "", false},
		{"2030-01-01", "2030-01-01", 1, "", false},
		{"2030-01-02", "2030-01-01", 1, "", false},
		{"2030-01-01", "2030-02-01", 1, "", false},
		{"2030-02-30", "2030-03-01", 1, "", false},
		{"2030-01-01", "2030-01-02", 0, "", false},
		{"2030-01-01", "2030-01-02", 5, "", false},
		{"2030-01-01", "2030-01-02", 2, "suite", false},
	} {
		s := Stay{HotelID: "hotel", CheckIn: tc.in, CheckOut: tc.out, Guests: tc.guests, RoomType: tc.kind}
		if got := s.Validate(now) == nil; got != tc.valid {
			t.Fatalf("%+v valid=%v", tc, got)
		}
	}
	// The UTC day can differ from the caller's local day.
	s := Stay{HotelID: "hotel", CheckIn: "2030-01-01", CheckOut: "2030-01-02", Guests: 1}
	if err := s.Validate(now.In(time.FixedZone("east", 3600))); err != nil {
		t.Fatal(err)
	}
}
