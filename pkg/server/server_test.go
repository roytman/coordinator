package server

import (
	"net/http"
	"strings"
	"testing"

	reqcommon "github.com/llm-d/llm-d-router/pkg/common/request"
)

func TestPickHeaders_RedactsInvalidRequestID(t *testing.T) {
	// The request-logging middleware runs before the handler validates the
	// request ID, so it must redact a malformed value rather than log raw
	// attacker-controlled input.
	h := http.Header{}
	malicious := "evil\r\nInjected: value"
	h.Set(reqcommon.RequestIDHeaderKey, malicious)

	out := pickHeaders(h, loggedRequestHeaders)
	if got := out[reqcommon.RequestIDHeaderKey]; strings.Contains(got, "Injected") || got == malicious {
		t.Fatalf("invalid request ID must be redacted, got %q", got)
	}
}

func TestPickHeaders_PassesValidRequestID(t *testing.T) {
	h := http.Header{}
	h.Set(reqcommon.RequestIDHeaderKey, "req-abc-123")

	out := pickHeaders(h, loggedRequestHeaders)
	if got := out[reqcommon.RequestIDHeaderKey]; got != "req-abc-123" {
		t.Fatalf("valid request ID must pass through, got %q", got)
	}
}

func TestPickHeaders_PassesOtherHeadersVerbatim(t *testing.T) {
	// Only the request ID is validated; other logged headers (whose values may
	// legitimately contain characters the request-ID regex rejects) pass through.
	h := http.Header{}
	h.Set("Content-Type", "application/json")

	out := pickHeaders(h, loggedRequestHeaders)
	if got := out["Content-Type"]; got != "application/json" {
		t.Fatalf("non request-ID header must pass through, got %q", got)
	}
}
