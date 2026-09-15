package agent

import (
	"context"
	"errors"
	"testing"

	"vivatom-api-svc/internal/ai"
	"vivatom-api-svc/internal/domain"
	"vivatom-api-svc/internal/generation"
)

type rejectingGuard struct{}

func (rejectingGuard) Check(domain.ProjectSnapshot) (domain.ProjectSnapshot, error) {
	return domain.ProjectSnapshot{}, errors.New("rejected")
}

func TestPlanEventOrder(t *testing.T) {
	provider := &ai.FakeProvider{}
	orchestrator := NewOrchestrator(provider, generation.NewGuard())
	events, err := orchestrator.Run(context.Background(), domain.AgentRequest{
		Action: domain.ActionPlan, ProjectID: "p1", Prompt: "任务看板",
	})
	if err != nil {
		t.Fatal(err)
	}

	var types []string
	for event := range events {
		types = append(types, event.Type)
	}
	want := []string{
		"agent.started",
		"action.status",
		"agent.output",
		"action.status",
		"agent.completed",
		"approval.required",
		"done",
	}
	if len(types) != len(want) {
		t.Fatalf("event types = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("event %d = %q, want %q", i, types[i], want[i])
		}
	}
}

func TestCanceledRunClosesEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := ai.NewFakeProvider()
	orchestrator := NewOrchestrator(provider, generation.NewGuard())
	events, err := orchestrator.Run(ctx, domain.AgentRequest{
		Action: domain.ActionPlan, ProjectID: "p1", Prompt: "任务看板",
	})
	if err != nil {
		t.Fatal(err)
	}

	<-events
	cancel()
	for range events {
	}
}

func TestBuildReturnsCandidateSnapshot(t *testing.T) {
	provider := &ai.FakeProvider{}
	orchestrator := NewOrchestrator(provider, generation.NewGuard())
	plan, err := provider.Plan(context.Background(), "任务看板")
	if err != nil {
		t.Fatal(err)
	}
	events, err := orchestrator.Run(context.Background(), domain.AgentRequest{
		Action: domain.ActionBuild, ProjectID: "p1", Prompt: "任务看板", Plan: &plan,
	})
	if err != nil {
		t.Fatal(err)
	}

	var snapshot *domain.ProjectSnapshot
	var lastType string
	for event := range events {
		lastType = event.Type
		if event.Type == "snapshot.completed" {
			snapshot = event.Snapshot
		}
	}
	if snapshot == nil {
		t.Fatal("build did not emit snapshot.completed")
	}
	if snapshot.EntryFile != "/src/main.ts" || len(snapshot.Files) != 3 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if lastType != "done" {
		t.Fatalf("last event = %q, want done", lastType)
	}
}

func TestBuildNeverEmitsRejectedSnapshot(t *testing.T) {
	provider := &ai.FakeProvider{}
	orchestrator := NewOrchestrator(provider, rejectingGuard{})
	plan, err := provider.Plan(context.Background(), "任务看板")
	if err != nil {
		t.Fatal(err)
	}
	events, err := orchestrator.Run(context.Background(), domain.AgentRequest{
		Action: domain.ActionBuild, ProjectID: "p1", Prompt: "任务看板", Plan: &plan,
	})
	if err != nil {
		t.Fatal(err)
	}

	var sawRejection bool
	for event := range events {
		if event.Type == "snapshot.completed" {
			t.Fatal("orchestrator emitted a rejected snapshot")
		}
		if event.Type == "error" && event.Code == "snapshot_rejected" {
			sawRejection = true
		}
	}
	if !sawRejection {
		t.Fatal("orchestrator did not report snapshot_rejected")
	}
}

func TestRevisionActionsReturnGuardedCompleteSnapshots(t *testing.T) {
	provider := &ai.FakeProvider{}
	orchestrator := NewOrchestrator(provider, generation.NewGuard())
	base, err := provider.Build(context.Background(), "任务板", domain.BuildPlan{})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []domain.AgentAction{domain.ActionIterate, domain.ActionRepair, domain.ActionPolish} {
		request := domain.AgentRequest{Action: action, ProjectID: "p1", Prompt: "增加筛选", Error: "修复筛选", Snapshot: &base}
		events, runErr := orchestrator.Run(context.Background(), request)
		if runErr != nil {
			t.Fatalf("%s: %v", action, runErr)
		}
		var candidate *domain.ProjectSnapshot
		for event := range events {
			if event.Type == "snapshot.completed" {
				candidate = event.Snapshot
			}
		}
		if candidate == nil || candidate.EntryFile != base.EntryFile || len(candidate.Files) <= len(base.Files) {
			t.Fatalf("%s returned incomplete candidate: %+v", action, candidate)
		}
	}
}
