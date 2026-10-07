package eventhandler

import (
	"context"
	"encoding/json"
	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/google/uuid"
	"github.com/sses79/hotel-booking-go/internal/events"
	"strings"
	"testing"
	"time"
)

type processor struct{ ids map[string]bool }

func (p *processor) Process(_ context.Context, e events.Created) error {
	p.ids[e.EventID] = true
	return nil
}
func TestCloudEventValidation(t *testing.T) {
	e := events.Created{Type: events.CreatedType, EventID: uuid.NewString(), BookingID: uuid.NewString(), Reference: "HB-" + strings.Repeat("A", 32), HotelID: uuid.NewString(), RoomID: uuid.NewString(), CreatedAt: time.Now().UTC()}
	b, _ := json.Marshal(e)
	envelope := Envelope{}
	envelope.Message.Data = b
	envelope.Message.ID = "message"
	cloud := event.New()
	cloud.SetType(PubSubType)
	cloud.SetID("cloud")
	cloud.SetSource("local")
	if err := cloud.SetData("application/json", envelope); err != nil {
		t.Fatal(err)
	}
	p := &processor{ids: map[string]bool{}}
	for range 2 {
		if err := Handle(context.Background(), cloud, p); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.ids) != 1 {
		t.Fatal("event identity not preserved")
	}
	cloud.SetType("wrong")
	if err := Handle(context.Background(), cloud, p); err == nil {
		t.Fatal("wrong type accepted")
	}
	cloud.SetType(PubSubType)
	_ = cloud.SetData("application/json", map[string]any{"message": map[string]string{"data": "e30="}})
	if err := Handle(context.Background(), cloud, p); err == nil {
		t.Fatal("invalid event accepted")
	}
}
