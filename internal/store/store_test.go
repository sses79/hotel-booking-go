package store

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestConnectionErrorsDoNotExposeCredentials(t *testing.T) {
	password := "secret@:/?#& value"
	dsn := "oracle://user:" + url.QueryEscape(password) + "@oracle:1521/FREEPDB1"
	for _, message := range []string{dsn, password, url.QueryEscape(password), url.PathEscape(password)} {
		redacted := redactConnectionError(errors.New("connection failed: "+message), dsn, password).Error()
		if strings.Contains(redacted, "secret") || !strings.Contains(redacted, "connection failed") {
			t.Fatalf("unsafe or unhelpful error: %s", redacted)
		}
	}
}
