package httpapi

import (
	"github.com/sses79/hotel-booking-go/api"
	"net/http"
)

func serveSpec(w http.ResponseWriter, r *http.Request) {
	payload, _ := api.Files.ReadFile("openapi.yaml")
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(payload)
}
func serveDocs(w http.ResponseWriter, r *http.Request) {
	payload, _ := api.Files.ReadFile("assets/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(payload)
}
