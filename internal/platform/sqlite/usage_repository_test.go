package sqlite

import (
	"context"
	"errors"
	"strings"
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
	repository := NewUsageRepository(database)
	service := usage.NewService(identityService, agent.NewOrchestrator(provider, generation.NewGuard()), repository)
	workspaceID := owner.Workspaces[0].ID
	plan, err := provider.Plan(ctx, "build")
	if err != nil {
		t.Fatal(err)
	}

	for index := 0; index < 3; index++ {
		approvalID, storeErr := repository.StorePlan(ctx, workspaceID, owner.User.ID, "p1", plan, "2026-01-01T00:00:00Z")
		if storeErr != nil {
			t.Fatal(storeErr)
		}
		if approveErr := service.Approve(ctx, owner.Session.Token, workspaceID, "p1", approvalID); approveErr != nil {
			t.Fatal(approveErr)
		}
		events, runErr := service.Run(ctx, owner.Session.Token, workspaceID, domain.AgentRequest{Action: domain.ActionBuild, ProjectID: "p1", ApprovalID: approvalID, Prompt: "build", Plan: &domain.BuildPlan{}})
		if runErr != nil {
			t.Fatal(runErr)
		}
		var candidateID, snapshotHash string
		for event := range events {
			if event.Type == "snapshot.completed" {
				candidateID, snapshotHash = event.CandidateID, event.SnapshotHash
			}
		}
		if !strings.HasPrefix(candidateID, "candidate_") || len(snapshotHash) != 64 {
			t.Fatalf("invalid candidate receipt: id=%q hash=%q", candidateID, snapshotHash)
		}
		var storedHash string
		if err = database.QueryRow(`SELECT snapshot_hash FROM build_candidates WHERE id=? AND workspace_id=? AND project_id=?`, candidateID, workspaceID, "p1").Scan(&storedHash); err != nil || storedHash != snapshotHash {
			t.Fatalf("candidate was not persisted: hash=%q err=%v", storedHash, err)
		}
		if index == 0 {
			version, commitErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build")
			if commitErr != nil || !strings.HasPrefix(version.ID, "version_") || version.CandidateID != candidateID || version.SnapshotHash != snapshotHash {
				t.Fatalf("commit candidate: version=%+v err=%v", version, commitErr)
			}
			retried, retryErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build")
			if retryErr != nil || retried.ID != version.ID {
				t.Fatalf("idempotent commit: version=%+v err=%v", retried, retryErr)
			}
			_, changedErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "changed prompt")
			assertUsageCode(t, changedErr, "candidate_invalid")
			_, reuseErr := service.Run(ctx, owner.Session.Token, workspaceID, domain.AgentRequest{Action: domain.ActionBuild, ProjectID: "p1", ApprovalID: approvalID, Prompt: "build"})
			assertUsageCode(t, reuseErr, "approval_invalid")
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
	approvalID, err := repository.StorePlan(ctx, workspaceID, owner.User.ID, "p1", plan, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Approve(ctx, owner.Session.Token, workspaceID, "p1", approvalID); err != nil {
		t.Fatal(err)
	}
	_, err = service.Run(ctx, owner.Session.Token, workspaceID, domain.AgentRequest{Action: domain.ActionBuild, ProjectID: "p1", ApprovalID: approvalID, Prompt: "build"})
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
