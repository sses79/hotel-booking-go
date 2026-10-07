package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/sses79/hotel-booking-go/internal/booking"
	"github.com/sses79/hotel-booking-go/internal/store"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func apiURL(t *testing.T) string {
	t.Helper()
	if os.Getenv("HTTP_INTEGRATION") != "1" {
		t.Skip("enable HTTP_INTEGRATION for the running Compose API")
	}
	base := os.Getenv("API_BASE_URL")
	if base == "" {
		base = "http://api:8080"
	}
	return strings.TrimRight(base, "/")
}
func call(base, method, path string, payload any) (int, []byte, http.Header, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, nil, err
		}
		body = bytes.NewReader(b)
	}
	r, err := http.NewRequest(method, base+path, body)
	if err != nil {
		return 0, nil, nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 40 * time.Second}
	resp, err := c.Do(r)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header, err
}
func mustCall(t *testing.T, base, method, path string, payload any, want int, dest any) http.Header {
	t.Helper()
	code, b, headers, err := call(base, method, path, payload)
	if err != nil || code != want {
		t.Fatalf("%s %s: status=%d want=%d body=%s error=%v", method, path, code, want, b, err)
	}
	if dest != nil {
		if err := json.Unmarshal(b, dest); err != nil {
			t.Fatal(err)
		}
	}
	return headers
}
func stay() booking.Stay {
	in := time.Now().UTC().AddDate(0, 0, 30)
	return booking.Stay{HotelID: store.DemoHotelID, CheckIn: in.Format(time.DateOnly), CheckOut: in.AddDate(0, 0, 3).Format(time.DateOnly), Guests: 2}
}
func availabilityPath(s booking.Stay) string {
	q := url.Values{"check_in": {s.CheckIn}, "check_out": {s.CheckOut}, "guests": {fmt.Sprint(s.Guests)}}
	if s.RoomType != "" {
		q.Set("room_type", s.RoomType)
	}
	return "/api/v1/hotels/" + s.HotelID + "/rooms/available?" + q.Encode()
}
func TestHTTPDemo(t *testing.T) {
	base := apiURL(t)
	mustCall(t, base, "GET", "/health/ready", nil, 200, nil)
	mustCall(t, base, "POST", "/api/v1/admin/reset", nil, 204, nil)
	mustCall(t, base, "POST", "/api/v1/admin/seed", nil, 200, nil)
	var hotels []store.Hotel
	mustCall(t, base, "GET", "/api/v1/hotels?name=Grand", nil, 200, &hotels)
	if len(hotels) != 1 || hotels[0].ID != store.DemoHotelID {
		t.Fatal(hotels)
	}
	s := stay()
	var rooms []booking.Room
	mustCall(t, base, "GET", availabilityPath(s), nil, 200, &rooms)
	if len(rooms) != 4 || rooms[0].Number != "201" {
		t.Fatal(rooms)
	}
	var d booking.Details
	headers := mustCall(t, base, "POST", "/api/v1/bookings", booking.Request{Stay: s, GuestName: "Demo Guest"}, 201, &d)
	if d.RoomNumber != "201" || !strings.HasPrefix(d.Reference, "HB-") || len(d.Reference) != 35 {
		t.Fatal(d)
	}
	var looked booking.Details
	mustCall(t, base, "GET", headers.Get("Location"), nil, 200, &looked)
	if looked.Reference != d.Reference || looked.HotelName != store.DemoHotelName {
		t.Fatal(looked)
	}
	mustCall(t, base, "GET", availabilityPath(s), nil, 200, &rooms)
	for _, r := range rooms {
		if r.Number == "201" {
			t.Fatal("booked room remains available")
		}
	}
	// Two real HTTP requests race for the last room suitable for four guests.
	s.Guests = 4
	s.RoomType = "deluxe"
	mustCall(t, base, "POST", "/api/v1/bookings", booking.Request{Stay: s, GuestName: "First Deluxe Guest"}, 201, nil)
	codes := make(chan int, 2)
	errs := make(chan error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			code, _, _, err := call(base, "POST", "/api/v1/bookings", booking.Request{Stay: s, GuestName: "Concurrent Guest"})
			codes <- code
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(codes)
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for code := range codes {
		if code == 201 {
			success++
		} else if code == 409 {
			conflict++
		} else {
			t.Fatalf("unexpected concurrent status %d", code)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	mustCall(t, base, "GET", "/openapi.yaml", nil, 200, nil)
	mustCall(t, base, "GET", "/docs", nil, 200, nil)
	mustCall(t, base, "GET", "/docs/assets/swagger-ui-bundle.js", nil, 200, nil)
	mustCall(t, base, "POST", "/api/v1/admin/reset", nil, 204, nil)
	mustCall(t, base, "GET", "/api/v1/hotels?name=Grand", nil, 200, &hotels)
	if len(hotels) != 0 {
		t.Fatal("reset did not clear hotels")
	}
	t.Log("complete HTTP demo and last-room concurrency check passed")
}
