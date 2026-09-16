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

type contractBreakingProvider struct{ *ai.FakeProvider }

type invalidPlanProvider struct{ *ai.FakeProvider }

func (p invalidPlanProvider) Plan(ctx context.Context, prompt string) (domain.BuildPlan, error) {
	plan, err := p.FakeProvider.Plan(ctx, prompt)
	plan.FilePlan = append(plan.FilePlan, plan.FilePlan[0])
	return plan, err
}

func (p contractBreakingProvider) Build(ctx context.Context, prompt string, plan domain.BuildPlan) (domain.ProjectSnapshot, error) {
	snapshot, err := p.FakeProvider.Build(ctx, prompt, plan)
	delete(snapshot.Files, "/src/App.tsx")
	return snapshot, err
}

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
	var agents []string
	for event := range events {
		types = append(types, event.Type)
		if event.Type == "agent.started" {
			agents = append(agents, event.Agent)
		}
	}
	want := []string{"agent.started", "action.status", "agent.output", "action.status", "agent.completed", "agent.started", "agent.completed", "agent.started", "action.status", "action.status", "agent.completed", "approval.required", "done"}
	if len(types) != len(want) {
		t.Fatalf("event types = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("event %d = %q, want %q", i, types[i], want[i])
		}
	}
	if len(agents) != 3 || agents[0] != "mike" || agents[1] != "emma" || agents[2] != "bob" {
		t.Fatalf("planning agents = %v", agents)
	}
}

func TestInvalidPlanNeverReachesApproval(t *testing.T) {
	orchestrator := NewOrchestrator(invalidPlanProvider{FakeProvider: &ai.FakeProvider{}}, generation.NewGuard())
	events, err := orchestrator.Run(context.Background(), domain.AgentRequest{Action: domain.ActionPlan, ProjectID: "p1", Prompt: "任务板"})
	if err != nil {
		t.Fatal(err)
	}
	var rejected bool
	for event := range events {
		if event.Type == "approval.required" {
			t.Fatal("invalid plan reached approval")
		}
		if event.Type == "error" && event.Code == "plan_rejected" {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("missing plan_rejected event")
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
	agents := make(map[string]bool)
	for event := range events {
		lastType = event.Type
		if event.Agent != "" {
			agents[event.Agent] = true
		}
		if event.Type == "snapshot.completed" {
			snapshot = event.Snapshot
		}
	}
	if snapshot == nil {
		t.Fatal("build did not emit snapshot.completed")
	}
	if snapshot.EntryFile != "/src/App.tsx" || len(snapshot.Files) != 3 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if lastType != "done" {
		t.Fatalf("last event = %q, want done", lastType)
	}
	for _, role := range []string{"alex"} {
		if !agents[role] {
			t.Fatalf("missing build agent %q in %v", role, agents)
		}
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

func TestBuildNeverEmitsSnapshotThatBreaksApprovedPlan(t *testing.T) {
	provider := contractBreakingProvider{FakeProvider: &ai.FakeProvider{}}
	orchestrator := NewOrchestrator(provider, generation.NewGuard())
	plan, err := provider.Plan(context.Background(), "任务看板")
	if err != nil {
		t.Fatal(err)
	}
	events, err := orchestrator.Run(context.Background(), domain.AgentRequest{Action: domain.ActionBuild, ProjectID: "p1", Prompt: "任务看板", Plan: &plan})
	if err != nil {
		t.Fatal(err)
	}
	var rejected bool
	for event := range events {
		if event.Type == "snapshot.completed" {
			t.Fatal("contract-breaking snapshot was emitted")
		}
		if event.Type == "error" && event.Code == "contract_rejected" {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("missing contract_rejected event")
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

func TestRaceReturnsTwoOrderedCandidates(t *testing.T) {
	provider := &ai.FakeProvider{}
	orchestrator := NewOrchestrator(provider, generation.NewGuard())
	plan, err := provider.Plan(context.Background(), "收入分析 Dashboard")
	if err != nil {
		t.Fatal(err)
	}
	events, err := orchestrator.Run(context.Background(), domain.AgentRequest{Action: domain.ActionRace, Mode: domain.ModeRace, ProjectID: "p1", Prompt: "收入分析 Dashboard", Plan: &plan})
	if err != nil {
		t.Fatal(err)
	}
	var candidates []domain.RaceCandidate
	for event := range events {
		if event.Type == "snapshot.completed" {
			candidates = event.Candidates
		}
	}
	if len(candidates) != 2 || candidates[0].ID != "candidate-a" || candidates[1].ID != "candidate-b" {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}
}
