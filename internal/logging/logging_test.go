package logging

import (
	"net/http"
	"strings"
	"testing"
)

func TestErrorPreviewRedactsSecrets(t *testing.T) {
	body := []byte(`{"api_key":"secret-key","message":"Bearer hidden-token"}`)
	preview := ErrorPreview(body, 4096)
	if strings.Contains(preview, "secret-key") || strings.Contains(preview, "hidden-token") {
		t.Fatalf("preview leaked a secret: %s", preview)
	}
}

func TestSafeHeaders(t *testing.T) {
	headers := http.Header{"Authorization": {"Bearer secret"}, "X-Request-Id": {"123"}}
	safe := SafeHeaders(headers)
	if _, ok := safe["Authorization"]; ok || safe["X-Request-Id"][0] != "123" {
		t.Fatalf("safe headers = %#v", safe)
	}
}
