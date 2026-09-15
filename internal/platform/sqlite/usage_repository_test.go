package sqlite

import (
	"context"
	"errors"
	"testing"

	"vivatom-api-svc/internal/agent"
	"vivatom-api-svc/internal/ai"
	"vivatom-api-svc/internal/domain"
	"vivatom-api-svc/internal/generation"
	"vivatom-api-svc/internal/identity"
	"vivatom-api-svc/internal/usage"
)

func TestUsageReservesCreditsAndEnforcesWorkspaceLimit(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	identityService := identity.NewService(NewIdentityRepository(database))
	owner, _ := identityService.Register(ctx, identity.Registration{Email: "usage-owner@example.com", Password: "password-one", Name: "Owner", WorkspaceName: "Usage"})
	other, _ := identityService.Register(ctx, identity.Registration{Email: "usage-other@example.com", Password: "password-two", Name: "Other", WorkspaceName: "Other"})
	provider := &ai.FakeProvider{}
	service := usage.NewService(identityService, agent.NewOrchestrator(provider, generation.NewGuard()), NewUsageRepository(database))
	workspaceID := owner.Workspaces[0].ID
	plan, err := provider.Plan(ctx, "build")
	if err != nil {
		t.Fatal(err)
	}

	for index := 0; index < 3; index++ {
		events, runErr := service.Run(ctx, owner.Session.Token, workspaceID, domain.AgentRequest{Action: domain.ActionBuild, ProjectID: "p1", Prompt: "build", Plan: &plan})
		if runErr != nil {
			t.Fatal(runErr)
		}
		for range events {
		}
	}
	events, err := service.Run(ctx, owner.Session.Token, workspaceID, domain.AgentRequest{Action: domain.ActionPlan, ProjectID: "p1", Prompt: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	summary, err := service.Summary(ctx, owner.Session.Token, workspaceID)
	if err != nil || summary.Used != 13 || summary.Remaining != 2 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	_, err = service.Run(ctx, owner.Session.Token, workspaceID, domain.AgentRequest{Action: domain.ActionBuild, ProjectID: "p1", Prompt: "build", Plan: &plan})
	assertUsageCode(t, err, "quota_exhausted")
	_, err = service.Summary(ctx, other.Session.Token, workspaceID)
	assertUsageCode(t, err, "workspace_forbidden")
	var succeeded int
	if err = database.QueryRow(`SELECT count(*) FROM agent_usage WHERE workspace_id=? AND status='succeeded'`, workspaceID).Scan(&succeeded); err != nil || succeeded != 4 {
		t.Fatalf("succeeded=%d err=%v", succeeded, err)
	}
}

func assertUsageCode(t *testing.T, err error, want string) {
	t.Helper()
	var value *usage.Error
	if !errors.As(err, &value) || value.Code != want {
		t.Fatalf("want %s, got %v", want, err)
	}
}
