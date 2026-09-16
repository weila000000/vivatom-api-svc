package sqlite

import (
	"context"
	"encoding/json"
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

func TestCompleteUsageAppendsOneTerminalAuditEvent(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	identityService := identity.NewService(NewIdentityRepository(database))
	owner, err := identityService.Register(ctx, identity.Registration{Email: "audit-usage@example.com", Password: "password-one", Name: "Owner", WorkspaceName: "Audit"})
	if err != nil {
		t.Fatal(err)
	}
	repository := NewUsageRepository(database)
	workspaceID := owner.Workspaces[0].ID
	usageID, result, err := repository.Reserve(ctx, owner.User.ID, workspaceID, "project-1", "plan", 1, "2026-01-01T00:00:00Z")
	if err != nil || result != usage.ResultOK {
		t.Fatalf("reserve: id=%q result=%q err=%v", usageID, result, err)
	}
	if err = repository.Complete(ctx, usageID, "failed", "provider_timeout", "2026-01-01T00:01:00Z"); err != nil {
		t.Fatal(err)
	}
	if err = repository.Complete(ctx, usageID, "failed", "different_code", "2026-01-01T00:02:00Z"); err != nil {
		t.Fatal(err)
	}
	var metadataJSON string
	var count int
	if err = database.QueryRow(`SELECT count(*),max(metadata_json) FROM audit_events WHERE action='agent.completed' AND target_id='project-1'`).Scan(&count, &metadataJSON); err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err = json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if count != 1 || metadata["status"] != "failed" || metadata["resultCode"] != "provider_timeout" || metadata["usageId"] != usageID {
		t.Fatalf("count=%d metadata=%v", count, metadata)
	}
}

type acceptingCompiler struct{}

func (acceptingCompiler) Compile(context.Context, domain.ProjectSnapshot) (domain.BuildVerification, error) {
	return domain.BuildVerification{Toolchain: "test-compiler", DurationMS: 10}, nil
}

type rejectingCompiler struct{}

func (rejectingCompiler) Compile(context.Context, domain.ProjectSnapshot) (domain.BuildVerification, error) {
	return domain.BuildVerification{}, rejectedCompileError{}
}

type rejectedCompileError struct{}

func (rejectedCompileError) Error() string         { return "compile failed" }
func (rejectedCompileError) CompileRejected() bool { return true }

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
	service := usage.NewService(identityService, agent.NewOrchestrator(provider, generation.NewGuard()), repository, acceptingCompiler{})
	workspaceID := owner.Workspaces[0].ID
	if _, err = database.Exec(`INSERT INTO workspace_projects (id,workspace_id,title,status,created_at,updated_at) VALUES (?,?,?,?,?,?)`, "p1", workspaceID, "Usage project", "building", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	plan, err := provider.Plan(ctx, "build")
	if err != nil {
		t.Fatal(err)
	}
	var committedVersionID string

	for index := 0; index < 3; index++ {
		approvalID, storeErr := repository.StorePlan(ctx, workspaceID, owner.User.ID, "p1", "build", plan, "2026-01-01T00:00:00Z")
		if storeErr != nil {
			t.Fatal(storeErr)
		}
		if approveErr := service.Approve(ctx, owner.Session.Token, workspaceID, "p1", approvalID); approveErr != nil {
			t.Fatal(approveErr)
		}
		_, changedPromptErr := service.Run(ctx, owner.Session.Token, workspaceID, domain.AgentRequest{Action: domain.ActionBuild, ProjectID: "p1", ApprovalID: approvalID, Prompt: "changed requirement"})
		assertUsageCode(t, changedPromptErr, "approval_invalid")
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
			_, promptErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "changed prompt")
			assertUsageCode(t, promptErr, "candidate_invalid")
			version, commitErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build")
			if commitErr != nil || !strings.HasPrefix(version.ID, "version_") || version.CandidateID != candidateID || version.SnapshotHash != snapshotHash || version.SourceAction != "build" || version.ApprovalID != approvalID || version.Safety == nil || version.Safety.Policy != "snapshot-guard/v1" || version.Build == nil || version.Build.Toolchain != "test-compiler" {
				t.Fatalf("commit candidate: version=%+v err=%v", version, commitErr)
			}
			committedVersionID = version.ID
			var activeVersionID, projectStatus string
			if err = database.QueryRow(`SELECT active_version_id,status FROM workspace_projects WHERE id=? AND workspace_id=?`, "p1", workspaceID).Scan(&activeVersionID, &projectStatus); err != nil || activeVersionID != version.ID || projectStatus != "ready" {
				t.Fatalf("version was not atomically activated: active=%q status=%q err=%v", activeVersionID, projectStatus, err)
			}
			var toolchain, verifiedAt string
			var durationMS int64
			if err = database.QueryRow(`SELECT toolchain,duration_ms,verified_at FROM build_verifications WHERE candidate_id=? AND snapshot_hash=?`, candidateID, snapshotHash).Scan(&toolchain, &durationMS, &verifiedAt); err != nil || toolchain != "test-compiler" || durationMS != 10 || verifiedAt == "" {
				t.Fatalf("verification was not persisted: toolchain=%q duration=%d verifiedAt=%q err=%v", toolchain, durationMS, verifiedAt, err)
			}
			retried, retryErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build")
			if retryErr != nil || retried.ID != version.ID {
				t.Fatalf("idempotent commit: version=%+v err=%v", retried, retryErr)
			}
			restaged, restageErr := service.RestageVersion(ctx, owner.Session.Token, workspaceID, "p1", version.ID)
			if restageErr != nil || !strings.HasPrefix(restaged.ID, "candidate_") || restaged.ID == candidateID || restaged.SnapshotHash != snapshotHash || restaged.Prompt != "恢复历史版本："+version.Snapshot.Title || restaged.Snapshot.Title != version.Snapshot.Title {
				t.Fatalf("restage version: candidate=%+v err=%v", restaged, restageErr)
			}
			restoredVersion, restoredErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", restaged.ID, restaged.SnapshotHash, version.ID, restaged.Prompt)
			if restoredErr != nil || restoredVersion.SourceAction != "restore" || restoredVersion.ApprovalID != "" || restoredVersion.Safety == nil || restoredVersion.Safety.Policy != version.Safety.Policy || restoredVersion.Safety.VerifiedAt != version.Safety.VerifiedAt {
				t.Fatalf("restored version provenance: version=%+v err=%v", restoredVersion, restoredErr)
			}
			committedVersionID = restoredVersion.ID
			_, restageOtherErr := service.RestageVersion(ctx, other.Session.Token, workspaceID, "p1", version.ID)
			assertUsageCode(t, restageOtherErr, "workspace_forbidden")
			_, missingVersionErr := service.RestageVersion(ctx, owner.Session.Token, workspaceID, "p1", "version_missing")
			assertUsageCode(t, missingVersionErr, "version_untrusted")
			_, changedErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "changed prompt")
			assertUsageCode(t, changedErr, "candidate_invalid")
			_, reuseErr := service.Run(ctx, owner.Session.Token, workspaceID, domain.AgentRequest{Action: domain.ActionBuild, ProjectID: "p1", ApprovalID: approvalID, Prompt: "build"})
			assertUsageCode(t, reuseErr, "approval_invalid")
		}
		if index == 1 {
			rejectingService := usage.NewService(identityService, agent.NewOrchestrator(provider, generation.NewGuard()), repository, rejectingCompiler{})
			_, compileErr := rejectingService.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build")
			assertUsageCode(t, compileErr, "compile_failed")
			var candidateStatus string
			var versionCount int
			if err = database.QueryRow(`SELECT status FROM build_candidates WHERE id=?`, candidateID).Scan(&candidateStatus); err != nil || candidateStatus != "pending" {
				t.Fatalf("failed build changed candidate: status=%q err=%v", candidateStatus, err)
			}
			if err = database.QueryRow(`SELECT count(*) FROM immutable_versions WHERE candidate_id=?`, candidateID).Scan(&versionCount); err != nil || versionCount != 0 {
				t.Fatalf("failed build created version: count=%d err=%v", versionCount, err)
			}
		}
		if index == 2 {
			_, conflictErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build")
			assertUsageCode(t, conflictErr, "version_conflict")
			var candidateStatus string
			if err = database.QueryRow(`SELECT status FROM build_candidates WHERE id=?`, candidateID).Scan(&candidateStatus); err != nil || candidateStatus != "rejected" {
				t.Fatalf("conflicting build changed candidate: status=%q err=%v", candidateStatus, err)
			}
			_, retryConflictErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, committedVersionID, "build")
			assertUsageCode(t, retryConflictErr, "candidate_invalid")
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
	approvalID, err := repository.StorePlan(ctx, workspaceID, owner.User.ID, "p1", "build", plan, "2026-01-01T00:00:00Z")
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
