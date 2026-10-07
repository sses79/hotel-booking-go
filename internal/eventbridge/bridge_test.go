package eventbridge

import (
	"encoding/json"
	"github.com/sses79/hotel-booking-go/internal/eventhandler"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWrappedPushTranslationAndRetryStatus(t *testing.T) {
	fixture := `{"message":{"data":"e30=","messageId":"123","publishTime":"2030-01-01T00:00:00Z"},"subscription":"projects/local-hotel/subscriptions/worker"}`
	for _, code := range []int{204, 503} {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			if string(b) != fixture || r.Header.Get("Ce-Specversion") != "1.0" || r.Header.Get("Ce-Type") != eventhandler.PubSubType || r.Header.Get("Ce-Id") != "123" {
				t.Error("incorrect CloudEvent translation")
			}
			w.WriteHeader(code)
		}))
		w := httptest.NewRecorder()
		New(target.URL, target.Client()).ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(fixture)))
		target.Close()
		if w.Code != code {
			t.Fatalf("code=%d want=%d", w.Code, code)
		}
	}
	w := httptest.NewRecorder()
	New("http://unused", http.DefaultClient).ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"message":{}}`)))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	var wrapped eventhandler.Envelope
	if err := json.Unmarshal([]byte(fixture), &wrapped); err != nil || string(wrapped.Message.Data) != "{}" {
		t.Fatal("fixture invalid")
	}
}
