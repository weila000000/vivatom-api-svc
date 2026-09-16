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

func TestRecoverInterruptedUsageAppendsTerminalAudit(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	identityService := identity.NewService(NewIdentityRepository(database))
	owner, err := identityService.Register(ctx, identity.Registration{Email: "recovery@example.com", Password: "password-one", Name: "Owner", WorkspaceName: "Recovery"})
	if err != nil {
		t.Fatal(err)
	}
	repository := NewUsageRepository(database)
	workspaceID := owner.Workspaces[0].ID
	usageID, result, err := repository.Reserve(ctx, owner.User.ID, workspaceID, "project-1", "plan", 1, "2026-01-01T00:00:00Z")
	if err != nil || result != usage.ResultOK {
		t.Fatalf("reserve: id=%q result=%q err=%v", usageID, result, err)
	}

	recovered, err := repository.RecoverInterrupted(ctx, "2026-01-01T00:01:00Z")
	if err != nil || recovered != 1 {
		t.Fatalf("recover: count=%d err=%v", recovered, err)
	}
	if recovered, err = repository.RecoverInterrupted(ctx, "2026-01-01T00:02:00Z"); err != nil || recovered != 0 {
		t.Fatalf("idempotent recover: count=%d err=%v", recovered, err)
	}
	var status, completedAt, resultCode string
	if err = database.QueryRow(`SELECT status,completed_at FROM agent_usage WHERE id=?`, usageID).Scan(&status, &completedAt); err != nil {
		t.Fatal(err)
	}
	if err = database.QueryRow(`SELECT json_extract(metadata_json,'$.resultCode') FROM audit_events WHERE action='agent.completed' AND json_extract(metadata_json,'$.usageId')=?`, usageID).Scan(&resultCode); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || completedAt != "2026-01-01T00:01:00Z" || resultCode != "server_restarted" {
		t.Fatalf("status=%q completedAt=%q resultCode=%q", status, completedAt, resultCode)
	}
}

func TestStoreCandidateRejectsSupersededPendingCandidate(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	identityService := identity.NewService(NewIdentityRepository(database))
	owner, err := identityService.Register(ctx, identity.Registration{Email: "candidate@example.com", Password: "password-one", Name: "Owner", WorkspaceName: "Candidates"})
	if err != nil {
		t.Fatal(err)
	}
	repository := NewUsageRepository(database)
	workspaceID := owner.Workspaces[0].ID
	usageID, result, err := repository.Reserve(ctx, owner.User.ID, workspaceID, "project-1", "build", 1, "2026-01-01T00:00:00Z")
	if err != nil || result != usage.ResultOK {
		t.Fatalf("reserve: result=%q err=%v", result, err)
	}
	snapshot := domain.ProjectSnapshot{Source: "model", Title: "Task", Summary: "Task", EntryFile: "/src/main.ts", Files: map[string]string{"/src/main.ts": "export {}"}, Dependencies: map[string]string{"vue": "3.5.42"}}
	firstID, _, err := repository.StoreCandidate(ctx, workspaceID, owner.User.ID, "project-1", usageID, "first", snapshot, "2026-01-01T00:01:00Z")
	if err != nil {
		t.Fatal(err)
	}
	var firstHash string
	if err = database.QueryRow(`SELECT snapshot_hash FROM build_candidates WHERE id=?`, firstID).Scan(&firstHash); err != nil {
		t.Fatal(err)
	}
	if leaseID, claimResult, claimErr := repository.ClaimCompilation(ctx, owner.User.ID, workspaceID, "project-1", firstID, firstHash, "2026-01-01T00:01:00Z", "2026-01-01T00:10:00Z"); claimErr != nil || claimResult != usage.ResultOK || leaseID == "" {
		t.Fatalf("claim first candidate: lease=%q result=%q err=%v", leaseID, claimResult, claimErr)
	}
	secondID, _, err := repository.StoreCandidate(ctx, workspaceID, owner.User.ID, "project-1", usageID, "second", snapshot, "2026-01-01T00:02:00Z")
	if err != nil {
		t.Fatal(err)
	}
	var firstStatus, secondStatus string
	if err = database.QueryRow(`SELECT status FROM build_candidates WHERE id=?`, firstID).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}
	if err = database.QueryRow(`SELECT status FROM build_candidates WHERE id=?`, secondID).Scan(&secondStatus); err != nil {
		t.Fatal(err)
	}
	if firstStatus != "rejected" || secondStatus != "pending" {
		t.Fatalf("candidate statuses: first=%q second=%q", firstStatus, secondStatus)
	}
	if _, err = database.Exec(`UPDATE safety_verifications SET policy='snapshot-guard/v1' WHERE candidate_id=?`, secondID); err != nil {
		t.Fatal(err)
	}
	if loaded, result, loadErr := repository.LoadCandidate(ctx, owner.User.ID, workspaceID, "project-1", secondID, firstHash, "second"); loadErr != nil || result != usage.ResultSafetyRejected || loaded != nil {
		t.Fatalf("stale safety policy loaded: snapshot=%+v result=%q err=%v", loaded, result, loadErr)
	}
	if _, err = database.Exec(`UPDATE safety_verifications SET policy=? WHERE candidate_id=?`, generation.PolicyVersion, secondID); err != nil {
		t.Fatal(err)
	}
	var leases int
	if err = database.QueryRow(`SELECT count(*) FROM candidate_compile_leases WHERE candidate_id=?`, firstID).Scan(&leases); err != nil || leases != 0 {
		t.Fatalf("superseded candidate leases=%d err=%v", leases, err)
	}
	tampered := snapshot
	tampered.Title = "Changed after verification"
	tamperedJSON, _ := json.Marshal(tampered)
	if _, err = database.Exec(`UPDATE build_candidates SET snapshot_json=? WHERE id=?`, string(tamperedJSON), secondID); err != nil {
		t.Fatal(err)
	}
	if loaded, result, loadErr := repository.LoadCandidate(ctx, owner.User.ID, workspaceID, "project-1", secondID, firstHash, "second"); loadErr != nil || result != usage.ResultCandidateInvalid || loaded != nil {
		t.Fatalf("tampered candidate loaded: snapshot=%+v result=%q err=%v", loaded, result, loadErr)
	}
	unsafe := snapshot
	unsafe.Files = map[string]string{"/src/main.ts": `fetch("https://example.test")`}
	if _, _, unsafeErr := repository.StoreCandidate(ctx, workspaceID, owner.User.ID, "project-1", usageID, "unsafe", unsafe, "2026-01-01T00:03:00Z"); unsafeErr == nil {
		t.Fatal("expected unsafe candidate storage to fail")
	}
}

type acceptingCompiler struct {
	calls       int
	verifyError error
}

func (c *acceptingCompiler) Compile(context.Context, domain.ProjectSnapshot) (domain.BuildVerification, error) {
	c.calls++
	return domain.BuildVerification{Toolchain: "test-compiler", DurationMS: 10, ArtifactID: strings.Repeat("a", 64)}, nil
}

func (c *acceptingCompiler) VerifyArtifact(context.Context, string) error { return c.verifyError }

type rejectingCompiler struct{}

func (rejectingCompiler) Compile(context.Context, domain.ProjectSnapshot) (domain.BuildVerification, error) {
	return domain.BuildVerification{}, rejectedCompileError{}
}
func (rejectingCompiler) VerifyArtifact(context.Context, string) error { return nil }

type unavailableCompiler struct{}

func (unavailableCompiler) Compile(context.Context, domain.ProjectSnapshot) (domain.BuildVerification, error) {
	return domain.BuildVerification{}, errors.New("worker offline")
}
func (unavailableCompiler) VerifyArtifact(context.Context, string) error {
	return errors.New("worker offline")
}

type busyCompiler struct{}

func (busyCompiler) Compile(context.Context, domain.ProjectSnapshot) (domain.BuildVerification, error) {
	return domain.BuildVerification{}, busyCompileError{}
}
func (busyCompiler) VerifyArtifact(context.Context, string) error { return nil }

type busyCompileError struct{}

func (busyCompileError) Error() string     { return "builder busy" }
func (busyCompileError) CompileBusy() bool { return true }

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
	compiler := &acceptingCompiler{}
	service := usage.NewService(identityService, agent.NewOrchestrator(provider, generation.NewGuard()), repository, compiler)
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
			if _, err = database.Exec(`UPDATE safety_verifications SET policy='snapshot-guard/v1' WHERE candidate_id=?`, candidateID); err != nil {
				t.Fatal(err)
			}
			_, unsafeCandidateErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build")
			assertUsageCode(t, unsafeCandidateErr, "candidate_unsafe")
			if compiler.calls != 0 {
				t.Fatalf("unsafe candidate invoked compiler %d times", compiler.calls)
			}
			if _, err = database.Exec(`UPDATE safety_verifications SET policy=? WHERE candidate_id=?`, generation.PolicyVersion, candidateID); err != nil {
				t.Fatal(err)
			}
			_, promptErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "changed prompt")
			assertUsageCode(t, promptErr, "candidate_invalid")
			version, commitErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build")
			if commitErr != nil || !strings.HasPrefix(version.ID, "version_") || version.CandidateID != candidateID || version.SnapshotHash != snapshotHash || version.SourceAction != "build" || version.ApprovalID != approvalID || version.Safety == nil || version.Safety.Policy != "snapshot-guard/v2" || version.Build == nil || version.Build.Toolchain != "test-compiler" {
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
			compiler.verifyError = errors.New("artifact missing")
			if _, artifactErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build"); artifactErr == nil {
				t.Fatal("expected missing artifact to block version reuse")
			} else {
				assertUsageCode(t, artifactErr, "artifact_unavailable")
			}
			compiler.verifyError = nil
			retried, retryErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build")
			if retryErr != nil || retried.ID != version.ID {
				t.Fatalf("idempotent commit: version=%+v err=%v", retried, retryErr)
			}
			if compiler.calls != 1 {
				t.Fatalf("idempotent commit compiled %d times", compiler.calls)
			}
			restaged, restageErr := service.RestageVersion(ctx, owner.Session.Token, workspaceID, "p1", version.ID)
			if restageErr != nil || !strings.HasPrefix(restaged.ID, "candidate_") || restaged.ID == candidateID || restaged.SnapshotHash != snapshotHash || restaged.Prompt != "恢复历史版本："+version.Snapshot.Title || restaged.Snapshot.Title != version.Snapshot.Title {
				t.Fatalf("restage version: candidate=%+v err=%v", restaged, restageErr)
			}
			restoredVersion, restoredErr := service.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", restaged.ID, restaged.SnapshotHash, version.ID, restaged.Prompt)
			if restoredErr != nil || restoredVersion.SourceAction != "restore" || restoredVersion.ApprovalID != "" || restoredVersion.Safety == nil || restoredVersion.Safety.Policy != generation.PolicyVersion || restoredVersion.Safety.VerifiedAt == "" {
				t.Fatalf("restored version provenance: version=%+v err=%v", restoredVersion, restoredErr)
			}
			committedVersionID = restoredVersion.ID
			_, restageOtherErr := service.RestageVersion(ctx, other.Session.Token, workspaceID, "p1", version.ID)
			assertUsageCode(t, restageOtherErr, "workspace_forbidden")
			_, missingVersionErr := service.RestageVersion(ctx, owner.Session.Token, workspaceID, "p1", "version_missing")
			assertUsageCode(t, missingVersionErr, "version_untrusted")
			unsafeSnapshot := version.Snapshot
			unsafeSnapshot.Files[unsafeSnapshot.EntryFile] = `fetch("https://example.test")`
			unsafeHash, unsafePayload, hashErr := domain.HashSnapshot(unsafeSnapshot)
			if hashErr != nil {
				t.Fatal(hashErr)
			}
			if _, err = database.Exec(`UPDATE immutable_versions SET snapshot_json=?,snapshot_hash=? WHERE id=?`, string(unsafePayload), unsafeHash, version.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = database.Exec(`UPDATE build_candidates SET snapshot_json=?,snapshot_hash=? WHERE id=?`, string(unsafePayload), unsafeHash, version.CandidateID); err != nil {
				t.Fatal(err)
			}
			if _, err = database.Exec(`UPDATE build_verifications SET snapshot_hash=? WHERE candidate_id=?`, unsafeHash, version.CandidateID); err != nil {
				t.Fatal(err)
			}
			if _, err = database.Exec(`UPDATE safety_verifications SET snapshot_hash=?,policy='snapshot-guard/v1' WHERE candidate_id=?`, unsafeHash, version.CandidateID); err != nil {
				t.Fatal(err)
			}
			_, unsafeRestageErr := service.RestageVersion(ctx, owner.Session.Token, workspaceID, "p1", version.ID)
			assertUsageCode(t, unsafeRestageErr, "version_unsafe")
			var restagedCandidates int
			if err = database.QueryRow(`SELECT count(*) FROM build_candidates WHERE usage_id=?`, "restore:"+version.ID).Scan(&restagedCandidates); err != nil || restagedCandidates != 1 {
				t.Fatalf("unsafe restage created a candidate: count=%d err=%v", restagedCandidates, err)
			}
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
			var attemptCode string
			if err = database.QueryRow(`SELECT result_code FROM build_attempts WHERE candidate_id=? AND status='failed'`, candidateID).Scan(&attemptCode); err != nil || attemptCode != "compile_failed" {
				t.Fatalf("failed build attempt: code=%q err=%v", attemptCode, err)
			}
			var auditCode string
			if err = database.QueryRow(`SELECT json_extract(metadata_json,'$.resultCode') FROM audit_events WHERE action='candidate.compile_failed' AND json_extract(metadata_json,'$.candidateId')=?`, candidateID).Scan(&auditCode); err != nil || auditCode != "compile_failed" {
				t.Fatalf("failed build audit: code=%q err=%v", auditCode, err)
			}
			unavailableService := usage.NewService(identityService, agent.NewOrchestrator(provider, generation.NewGuard()), repository, unavailableCompiler{})
			_, unavailableErr := unavailableService.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build")
			assertUsageCode(t, unavailableErr, "compiler_unavailable")
			var unavailableAttempts int
			if err = database.QueryRow(`SELECT count(*) FROM build_attempts WHERE candidate_id=? AND result_code='compiler_unavailable'`, candidateID).Scan(&unavailableAttempts); err != nil || unavailableAttempts != 1 {
				t.Fatalf("unavailable build attempt: count=%d err=%v", unavailableAttempts, err)
			}
			busyService := usage.NewService(identityService, agent.NewOrchestrator(provider, generation.NewGuard()), repository, busyCompiler{})
			_, busyErr := busyService.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, "", "build")
			assertUsageCode(t, busyErr, "compile_busy")
			var busyAttempts int
			if err = database.QueryRow(`SELECT count(*) FROM build_attempts WHERE candidate_id=? AND result_code='compile_busy'`, candidateID).Scan(&busyAttempts); err != nil || busyAttempts != 1 {
				t.Fatalf("busy build attempt: count=%d err=%v", busyAttempts, err)
			}
			leaseID, claimResult, claimErr := repository.ClaimCompilation(ctx, owner.User.ID, workspaceID, "p1", candidateID, snapshotHash, "2026-01-01T00:05:00Z", "2026-01-01T00:06:00Z")
			if claimErr != nil || claimResult != usage.ResultOK || leaseID == "" {
				t.Fatalf("claim compilation: lease=%q result=%q err=%v", leaseID, claimResult, claimErr)
			}
			if duplicateLease, duplicateResult, duplicateErr := repository.ClaimCompilation(ctx, owner.User.ID, workspaceID, "p1", candidateID, snapshotHash, "2026-01-01T00:05:30Z", "2026-01-01T00:07:00Z"); duplicateErr != nil || duplicateResult != usage.ResultCompileInProgress || duplicateLease != "" {
				t.Fatalf("duplicate claim: lease=%q result=%q err=%v", duplicateLease, duplicateResult, duplicateErr)
			}
			takeoverLease, takeoverResult, takeoverErr := repository.ClaimCompilation(ctx, owner.User.ID, workspaceID, "p1", candidateID, snapshotHash, "2026-01-01T00:06:00Z", "2026-01-01T00:08:00Z")
			if takeoverErr != nil || takeoverResult != usage.ResultOK || takeoverLease == "" || takeoverLease == leaseID {
				t.Fatalf("expired lease takeover: lease=%q result=%q err=%v", takeoverLease, takeoverResult, takeoverErr)
			}
			if _, result, verificationErr := repository.RecordVerification(ctx, owner.User.ID, workspaceID, "p1", candidateID, snapshotHash, takeoverLease, domain.BuildVerification{Toolchain: "recovered-compiler", DurationMS: 12}, "2026-01-01T00:10:00Z"); verificationErr != nil || result != usage.ResultOK {
				t.Fatalf("record interrupted verification: result=%q err=%v", result, verificationErr)
			}
			resumeCompiler := &acceptingCompiler{}
			resumeService := usage.NewService(identityService, agent.NewOrchestrator(provider, generation.NewGuard()), repository, resumeCompiler)
			recoveredVersion, resumeErr := resumeService.CommitCandidate(ctx, owner.Session.Token, workspaceID, "p1", candidateID, snapshotHash, committedVersionID, "build")
			if resumeErr != nil || recoveredVersion.Build == nil || recoveredVersion.Build.Toolchain != "recovered-compiler" {
				t.Fatalf("resume verified candidate: version=%+v err=%v", recoveredVersion, resumeErr)
			}
			committedVersionID = recoveredVersion.ID
			if err = database.QueryRow(`SELECT count(*) FROM build_attempts WHERE candidate_id=? AND result_code='compiler_unavailable'`, candidateID).Scan(&unavailableAttempts); err != nil || unavailableAttempts != 1 {
				t.Fatalf("resume repeated worker call: count=%d err=%v", unavailableAttempts, err)
			}
			if resumeCompiler.calls != 0 {
				t.Fatalf("resume recompiled verified candidate %d times", resumeCompiler.calls)
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
	var usageID string
	if err = database.QueryRow(`SELECT usage_id FROM build_candidates WHERE project_id='p1' ORDER BY created_at DESC LIMIT 1`).Scan(&usageID); err != nil {
		t.Fatal(err)
	}
	replacementSnapshot, err := provider.Build(ctx, "replacement", plan)
	if err != nil {
		t.Fatal(err)
	}
	supersededID, _, err := repository.StoreCandidate(ctx, workspaceID, owner.User.ID, "p1", usageID, "replacement", replacementSnapshot, "2026-01-01T00:20:00Z")
	if err != nil {
		t.Fatal(err)
	}
	latestID, _, err := repository.StoreCandidate(ctx, workspaceID, owner.User.ID, "p1", usageID, "replacement", replacementSnapshot, "2026-01-01T00:21:00Z")
	if err != nil || latestID == supersededID {
		t.Fatalf("store replacement candidate: latest=%q superseded=%q err=%v", latestID, supersededID, err)
	}
	var supersededStatus, latestStatus string
	if err = database.QueryRow(`SELECT status FROM build_candidates WHERE id=?`, supersededID).Scan(&supersededStatus); err != nil {
		t.Fatal(err)
	}
	if err = database.QueryRow(`SELECT status FROM build_candidates WHERE id=?`, latestID).Scan(&latestStatus); err != nil {
		t.Fatal(err)
	}
	if supersededStatus != "rejected" || latestStatus != "pending" {
		t.Fatalf("candidate replacement statuses: superseded=%q latest=%q", supersededStatus, latestStatus)
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
	if _, err = database.Exec(`UPDATE workspaces SET credit_limit=credit_limit+99999 WHERE id=?`, workspaceID); err != nil {
		t.Fatal(err)
	}
	recharged, err := service.Summary(ctx, owner.Session.Token, workspaceID)
	if err != nil || recharged.Limit != 100014 || recharged.Used != 13 || recharged.Remaining != 100001 {
		t.Fatalf("recharged summary=%+v err=%v", recharged, err)
	}
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
