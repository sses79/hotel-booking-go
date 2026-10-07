package main

import (
	"github.com/sses79/hotel-booking-go/internal/eventbridge"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	target := os.Getenv("EVENT_TARGET_URL")
	if target == "" {
		log.Fatal("EVENT_TARGET_URL is required")
	}
	s := &http.Server{Addr: ":8080", Handler: eventbridge.New(target, &http.Client{Timeout: 25 * time.Second}), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(s.ListenAndServe())
}
