// An independently invoked publisher makes progress without API traffic.
package main

import (
	"context"
	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/events"
	"github.com/sses79/hotel-booking-go/internal/store"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	s, err := store.New(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	p, err := events.NewPubSub(ctx, os.Getenv("PUBSUB_PROJECT_ID"), os.Getenv("PUBSUB_TOPIC"))
	if err != nil {
		log.Fatal(err)
	}
	defer p.Close()
	interval := 5 * time.Second
	if v := os.Getenv("OUTBOX_INTERVAL"); v != "" {
		interval, err = time.ParseDuration(v)
		if err != nil || interval < time.Second {
			log.Fatal("OUTBOX_INTERVAL must be at least 1s")
		}
	}
	for {
		batch, cancel := context.WithTimeout(ctx, 30*time.Second)
		n, err := events.Replay(batch, s, p, 100)
		cancel()
		if err != nil {
			log.Printf("outbox retry pending: %v", err)
		} else if n > 0 {
			log.Printf("outbox delivered: %d", n)
		}
		if os.Getenv("OUTBOX_ONCE") == "1" {
			if err != nil {
				os.Exit(1)
			}
			return
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
