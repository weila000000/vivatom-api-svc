package compiler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vivatom-api-svc/internal/domain"
)

func TestClientAuthenticatesAndRequiresSuccessfulBuild(t *testing.T) {
	var receivedToken string
	var receivedSnapshotHash string
	artifactID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedToken = request.Header.Get("X-Vivatom-Builder-Token")
		if request.URL.Path != "/compile" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			SnapshotHash string `json:"snapshotHash"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		receivedSnapshotHash = body.SnapshotHash
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"data":{"toolchain":"vite@7.3.6+vue@3.5.42","durationMs":120,"artifactId":"`+artifactID+`"}}`)
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
	if receivedSnapshotHash == "" {
		t.Fatal("snapshot hash was not sent")
	}
	if verification.ArtifactID != artifactID {
		t.Fatalf("artifact id = %q", verification.ArtifactID)
	}

	failed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(writer, `{"error":"compile_failed","diagnostic":"App.vue:12 unexpected token"}`)
	}))
	defer failed.Close()
	if _, err := NewClient(failed.URL, "secret", time.Second).Compile(context.Background(), domain.ProjectSnapshot{}); err == nil || err.Error() != "App.vue:12 unexpected token" {
		t.Fatalf("expected compiler diagnostic, got %v", err)
	}
}

func TestClientVerifiesPersistedArtifact(t *testing.T) {
	artifactID := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/verify" || request.Header.Get("X-Vivatom-Builder-Token") != "builder-secret" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			ArtifactID string `json:"artifactId"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.ArtifactID != artifactID {
			t.Fatalf("artifact id = %q", body.ArtifactID)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := NewClient(server.URL, "builder-secret", time.Second).VerifyArtifact(context.Background(), artifactID); err != nil {
		t.Fatal(err)
	}
	if err := NewClient(server.URL, "builder-secret", time.Second).VerifyArtifact(context.Background(), "invalid"); err == nil {
		t.Fatal("expected invalid artifact id to be rejected")
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
