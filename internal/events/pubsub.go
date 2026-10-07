package events

import (
	"context"
	"fmt"

	"cloud.google.com/go/pubsub"
)

type PubSub struct {
	client *pubsub.Client
	topic  *pubsub.Topic
}

func NewPubSub(ctx context.Context, project, topic string) (*PubSub, error) {
	if project == "" || topic == "" {
		return nil, fmt.Errorf("PUBSUB_PROJECT_ID and PUBSUB_TOPIC are required")
	}
	c, err := pubsub.NewClient(ctx, project)
	if err != nil {
		return nil, err
	}
	t := c.Topic(topic)
	t.PublishSettings.CountThreshold = 1
	return &PubSub{client: c, topic: t}, nil
}
func (p *PubSub) Publish(ctx context.Context, data []byte) error {
	_, err := p.topic.Publish(ctx, &pubsub.Message{Data: data, Attributes: map[string]string{"type": CreatedType}}).Get(ctx)
	return err
}
func (p *PubSub) Close() { p.topic.Stop(); _ = p.client.Close() }
