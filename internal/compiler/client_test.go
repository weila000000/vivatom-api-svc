package compiler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vivatom-api-svc/internal/domain"
)

func TestClientAuthenticatesAndRequiresSuccessfulBuild(t *testing.T) {
	var receivedToken string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedToken = request.Header.Get("X-Vivatom-Builder-Token")
		if request.URL.Path != "/compile" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"data":{"toolchain":"vite@7.3.6+vue@3.5.42","durationMs":120}}`)
	}))
	defer server.Close()
	client := NewClient(server.URL, "builder-secret", time.Second)
	verification, err := client.Compile(context.Background(), domain.ProjectSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if verification.Toolchain != "vite@7.3.6+vue@3.5.42" || verification.DurationMS != 120 {
		t.Fatalf("verification = %+v", verification)
	}
	if receivedToken != "builder-secret" {
		t.Fatalf("builder token = %q", receivedToken)
	}

	failed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer failed.Close()
	if _, err := NewClient(failed.URL, "secret", time.Second).Compile(context.Background(), domain.ProjectSnapshot{}); err == nil {
		t.Fatal("expected compiler failure")
	}
}

func TestClientChecksBuilderReadiness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/health" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := NewClient(server.URL, "secret", time.Second).Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsUnhealthyBuilder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := NewClient(server.URL, "secret", time.Second).Ready(context.Background())
	if err == nil {
		t.Fatal("expected readiness failure")
	}
}

func TestClientHonorsReadinessContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		<-time.After(100 * time.Millisecond)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := NewClient(server.URL, "secret", time.Second).Ready(ctx)
	if err == nil {
		t.Fatal("expected readiness timeout")
	}
}
