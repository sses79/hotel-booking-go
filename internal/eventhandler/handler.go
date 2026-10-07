package eventhandler

import (
	"context"
	"fmt"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/sses79/hotel-booking-go/internal/events"
)

const PubSubType = "google.cloud.pubsub.topic.v1.messagePublished"

type Envelope struct {
	Message struct {
		Data []byte `json:"data"`
		ID   string `json:"messageId"`
	} `json:"message"`
	Subscription string `json:"subscription,omitempty"`
}
type Processor interface {
	Process(context.Context, events.Created) error
}

func Handle(ctx context.Context, cloud event.Event, processor Processor) error {
	if cloud.Type() != PubSubType {
		return fmt.Errorf("unsupported CloudEvent type %q", cloud.Type())
	}
	var wrapped Envelope
	if err := cloud.DataAs(&wrapped); err != nil {
		return err
	}
	e, err := events.Decode(wrapped.Message.Data)
	if err != nil {
		return err
	}
	return processor.Process(ctx, e)
}
