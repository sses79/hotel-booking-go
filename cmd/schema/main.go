package main

import (
	"context"
	"log"
	"time"

	"github.com/sses79/hotel-booking-go/internal/config"
	"github.com/sses79/hotel-booking-go/internal/dbschema"
	"github.com/sses79/hotel-booking-go/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if !cfg.DemoRoutesEnabled() {
		log.Fatal("schema setup is local/demo-only: APP_ENV must be local or demo")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := store.Open(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := dbschema.Apply(ctx, db, "schema"); err != nil {
		log.Fatal(err)
	}
	log.Print("Oracle application schema ready")
}
