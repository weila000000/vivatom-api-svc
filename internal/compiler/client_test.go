package compiler

import (
	"context"
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
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := NewClient(server.URL, "builder-secret", time.Second)
	if err := client.Compile(context.Background(), domain.ProjectSnapshot{}); err != nil {
		t.Fatal(err)
	}
	if receivedToken != "builder-secret" {
		t.Fatalf("builder token = %q", receivedToken)
	}

	failed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer failed.Close()
	if err := NewClient(failed.URL, "secret", time.Second).Compile(context.Background(), domain.ProjectSnapshot{}); err == nil {
		t.Fatal("expected compiler failure")
	}
}
