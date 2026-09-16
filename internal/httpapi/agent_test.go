package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vivatom-api-svc/internal/domain"
)

type stubRunner struct{}

func (stubRunner) Run(_ context.Context, _, _ string, request domain.AgentRequest) (<-chan domain.AgentEvent, error) {
	events := make(chan domain.AgentEvent, 2)
	events <- domain.AgentEvent{Type: "agent.started", Agent: "mike"}
	events <- domain.AgentEvent{Type: "done"}
	close(events)
	return events, request.Validate()
}

func TestAgentSSE(t *testing.T) {
	body := bytes.NewBufferString(`{"action":"plan","projectId":"p1","prompt":"任务板"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/platform/workspaces/w1/agent", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer token")
	recorder := httptest.NewRecorder()
	NewRouter(Dependencies{AgentRunner: stubRunner{}}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content type = %q", got)
	}

	scanner := bufio.NewScanner(recorder.Body)
	var names []string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: ") {
			names = append(names, strings.TrimPrefix(scanner.Text(), "event: "))
		}
		if strings.HasPrefix(scanner.Text(), "data: ") {
			var event domain.AgentEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &event); err != nil {
				t.Fatal(err)
			}
		}
	}
	if strings.Join(names, ",") != "agent.started,done" {
		t.Fatalf("events = %v", names)
	}
}

func TestAgentRejectsInvalidRequest(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/platform/workspaces/w1/agent", strings.NewReader(`{"action":"plan","projectId":"p1"}`))
	request.Header.Set("Authorization", "Bearer token")
	recorder := httptest.NewRecorder()
	NewRouter(Dependencies{AgentRunner: stubRunner{}}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestAgentStreamKeepsConnectionAlive(t *testing.T) {
	events := make(chan domain.AgentEvent)
	heartbeats := make(chan time.Time, 1)
	heartbeats <- time.Now()
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(events)
	}()

	var output bytes.Buffer
	flushes := 0
	streamAgentEvents(context.Background(), &output, func() { flushes++ }, events, heartbeats)

	stream := output.String()
	if !strings.Contains(stream, ": keepalive\n\n") {
		t.Fatalf("stream has no heartbeat: %q", stream)
	}
	if !strings.Contains(stream, "event: error\ndata: {\"type\":\"error\",\"message\":\"Agent 事件流意外中断\",\"code\":\"agent_stream_incomplete\",\"retryable\":true}") {
		t.Fatalf("stream has no incomplete error: %q", stream)
	}
	if !strings.HasSuffix(stream, "event: done\ndata: {\"type\":\"done\"}\n\n") {
		t.Fatalf("stream has no terminal event: %q", stream)
	}
	if flushes != 3 {
		t.Fatalf("flushes = %d, want 3", flushes)
	}
}
