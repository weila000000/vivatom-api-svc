package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthRoutes(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		ready  func() error
		status int
	}{
		{"live", "/api/health/live", nil, http.StatusOK},
		{"ready", "/api/health/ready", func() error { return nil }, http.StatusOK},
		{"not ready", "/api/health/ready", func() error { return errors.New("offline") }, http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			NewRouter(Dependencies{Ready: tt.ready}).ServeHTTP(recorder, request)
			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.status)
			}
		})
	}
}

func TestOperationalMiddleware(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(Dependencies{
		Logger:          slog.New(slog.NewJSONHandler(&logs, nil)),
		AuthRateLimit:   1,
		RateLimitWindow: time.Minute,
	})

	first := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/platform/auth/login", strings.NewReader(`{"email":"a@example.com","password":"password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "trace-from-client")
	router.ServeHTTP(first, request)
	if first.Header().Get("X-Request-ID") != "trace-from-client" {
		t.Fatalf("request id was not propagated: %q", first.Header().Get("X-Request-ID"))
	}

	second := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/platform/auth/login", strings.NewReader(`{"email":"a@example.com","password":"password"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(second, request)
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limited response = %d, retry-after = %q", second.Code, second.Header().Get("Retry-After"))
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil || body.Error.Code != "rate_limited" {
		t.Fatalf("unexpected limiter body: %s", second.Body.String())
	}
	if !strings.Contains(logs.String(), `"msg":"http_request"`) || !strings.Contains(logs.String(), `"request_id":"trace-from-client"`) {
		t.Fatalf("structured access log missing fields: %s", logs.String())
	}
}

func TestRequestIDRejectsUnsafeInput(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/health/live", nil)
	request.Header.Set("X-Request-ID", "unsafe request id")
	NewRouter(Dependencies{}).ServeHTTP(recorder, request)
	if value := recorder.Header().Get("X-Request-ID"); value == "unsafe request id" || value == "" {
		t.Fatalf("unsafe request id was not replaced: %q", value)
	}
}
