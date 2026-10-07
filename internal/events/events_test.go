package events

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func fixture() Created {
	return Created{Type: CreatedType, EventID: uuid.NewString(), BookingID: uuid.NewString(), Reference: "HB-" + strings.Repeat("A", 32), HotelID: uuid.NewString(), RoomID: uuid.NewString(), CreatedAt: time.Now().UTC()}
}

type memoryOutbox struct {
	event   Created
	marked  bool
	markErr error
}

func (m *memoryOutbox) Pending(context.Context, int) ([]Created, error) {
	if m.marked {
		return nil, nil
	}
	return []Created{m.event}, nil
}
func (m *memoryOutbox) MarkPublished(context.Context, string) error {
	if m.markErr != nil {
		return m.markErr
	}
	m.marked = true
	return nil
}

type fakePublisher struct {
	fail bool
	ids  []string
}

func (p *fakePublisher) Publish(_ context.Context, b []byte) error {
	if p.fail {
		return errors.New("offline")
	}
	e, err := Decode(b)
	if err != nil {
		return err
	}
	p.ids = append(p.ids, e.EventID)
	return nil
}
func TestSerializationAndRecovery(t *testing.T) {
	e := fixture()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(b)
	if err != nil || decoded.EventID != e.EventID {
		t.Fatalf("%+v %v", decoded, err)
	}
	if strings.Contains(string(b), "guest") {
		t.Fatal("event contains personal guest information")
	}
	m := &memoryOutbox{event: e}
	p := &fakePublisher{fail: true}
	ctx := context.Background()
	if err := Deliver(ctx, m, p, e); err == nil || m.marked {
		t.Fatal("failed publish must stay pending")
	}
	p.fail = false
	m.markErr = errors.New("database offline")
	if _, err := Replay(ctx, m, p, 100); err == nil || m.marked {
		t.Fatal("failed mark must stay pending")
	}
	m.markErr = nil
	if n, err := Replay(ctx, m, p, 100); err != nil || n != 1 || !m.marked {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(p.ids) != 2 || p.ids[0] != p.ids[1] {
		t.Fatal("retry changed event identity")
	}
	if n, err := Replay(ctx, m, p, 100); n != 0 || err != nil {
		t.Fatal("published row replayed")
	}
}
