package events

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

const CreatedType = "booking.created"

var referencePattern = regexp.MustCompile(`^HB-[0-9A-F]{32}$`)

type Created struct {
	Type      string    `json:"type"`
	EventID   string    `json:"event_id"`
	BookingID string    `json:"booking_id"`
	Reference string    `json:"reference"`
	HotelID   string    `json:"hotel_id"`
	RoomID    string    `json:"room_id"`
	CreatedAt time.Time `json:"created_at"`
}

func Decode(payload []byte) (Created, error) {
	var e Created
	if err := json.Unmarshal(payload, &e); err != nil {
		return e, err
	}
	if e.Type != CreatedType || e.CreatedAt.IsZero() || !referencePattern.MatchString(e.Reference) {
		return e, fmt.Errorf("invalid booking event")
	}
	for _, id := range []string{e.EventID, e.BookingID, e.HotelID, e.RoomID} {
		if _, err := uuid.Parse(id); err != nil {
			return e, fmt.Errorf("invalid event identifier: %w", err)
		}
	}
	return e, nil
}

type Publisher interface {
	Publish(context.Context, []byte) error
}
type Outbox interface {
	Pending(context.Context, int) ([]Created, error)
	MarkPublished(context.Context, string) error
}

// Deliver may publish twice if marking fails. Consumers deduplicate EventID.
func Deliver(ctx context.Context, outbox Outbox, publisher Publisher, e Created) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := publisher.Publish(ctx, payload); err != nil {
		return err
	}
	return outbox.MarkPublished(ctx, e.EventID)
}

func Replay(ctx context.Context, outbox Outbox, publisher Publisher, limit int) (int, error) {
	rows, err := outbox.Pending(ctx, limit)
	if err != nil {
		return 0, err
	}
	n := 0
	var first error
	for _, e := range rows {
		if err := Deliver(ctx, outbox, publisher, e); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		n++
	}
	return n, first
}
