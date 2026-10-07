// Package eventbridge adapts local emulator pushes; it is not an Eventarc emulator.
package eventbridge

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/sses79/hotel-booking-go/internal/eventhandler"
)

func New(target string, client *http.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "invalid push", 400)
			return
		}
		var wrapped eventhandler.Envelope
		if json.Unmarshal(payload, &wrapped) != nil || wrapped.Message.ID == "" || len(wrapped.Message.Data) == 0 {
			http.Error(w, "invalid push", 400)
			return
		}
		request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(payload))
		if err != nil {
			http.Error(w, "invalid target", 500)
			return
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Ce-Specversion", "1.0")
		request.Header.Set("Ce-Type", eventhandler.PubSubType)
		request.Header.Set("Ce-Id", wrapped.Message.ID)
		request.Header.Set("Ce-Source", "//pubsub.googleapis.com/projects/local-hotel/topics/booking-created")
		request.Header.Set("Ce-Time", time.Now().UTC().Format(time.RFC3339Nano))
		response, err := client.Do(request)
		if err != nil {
			http.Error(w, "worker unavailable", 503)
			return
		}
		defer response.Body.Close()
		w.WriteHeader(response.StatusCode)
	})
}
