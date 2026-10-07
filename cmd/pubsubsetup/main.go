package main

import (
	"cloud.google.com/go/pubsub"
	"context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"os"
	"time"
)

func main() {
	if os.Getenv("PUBSUB_EMULATOR_HOST") == "" {
		log.Fatal("local setup requires PUBSUB_EMULATOR_HOST")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := pubsub.NewClient(ctx, os.Getenv("PUBSUB_PROJECT_ID"))
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	name := os.Getenv("PUBSUB_TOPIC")
	t := c.Topic(name)
	for {
		_, err = c.CreateTopic(ctx, name)
		if err == nil || status.Code(err) == codes.AlreadyExists {
			break
		}
		if ctx.Err() != nil {
			log.Fatal(err)
		}
		time.Sleep(time.Second)
	}
	_, err = c.CreateSubscription(ctx, "booking-created-worker", pubsub.SubscriptionConfig{Topic: t, AckDeadline: 30 * time.Second, PushConfig: pubsub.PushConfig{Endpoint: "http://eventbridge:8080/"}})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		log.Fatal(err)
	}
}
