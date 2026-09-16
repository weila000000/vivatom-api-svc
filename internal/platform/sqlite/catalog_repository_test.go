package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"vivatom-api-svc/internal/audit"
	"vivatom-api-svc/internal/catalog"
	"vivatom-api-svc/internal/domain"
	"vivatom-api-svc/internal/identity"
)

func TestCatalogEnforcesWorkspaceMembershipAndProjectOwnership(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	ctx := context.Background()
	identityService := identity.NewService(NewIdentityRepository(database))
	catalogService := catalog.NewService(identityService, NewCatalogRepository(database))
	owner, err := identityService.Register(ctx, identity.Registration{Email: "owner@example.com", Password: "password-one", Name: "Owner", WorkspaceName: "Owner Space"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := identityService.Register(ctx, identity.Registration{Email: "other@example.com", Password: "password-two", Name: "Other", WorkspaceName: "Other Space"})
	if err != nil {
		t.Fatal(err)
	}

	input := catalog.SyncInput{ID: "project-1", Title: "任务看板", Status: "planning"}
	created, err := catalogService.Sync(ctx, owner.Session.Token, owner.Workspaces[0].ID, input)
	if err != nil || created.WorkspaceID != owner.Workspaces[0].ID {
		t.Fatalf("create failed: project=%+v err=%v", created, err)
	}
	input.Status = "ready"
	versionID := "version-1"
	input.ActiveVersionID = &versionID
	_, err = catalogService.Sync(ctx, owner.Session.Token, owner.Workspaces[0].ID, input)
	assertCatalogCode(t, err, "active_version_conflict")
	registerDocumentVersion(t, database, owner.Workspaces[0].ID, owner.User.ID, input.ID, versionID, "", "构建任务板", "2026-01-01T00:00:00Z", documentSnapshot())
	if _, err = database.Exec(`UPDATE workspace_projects SET active_version_id=?,status='ready' WHERE id=? AND workspace_id=?`, versionID, input.ID, owner.Workspaces[0].ID); err != nil {
		t.Fatal(err)
	}
	updated, err := catalogService.Sync(ctx, owner.Session.Token, owner.Workspaces[0].ID, input)
	if err != nil || updated.Status != "ready" || updated.CreatedAt != created.CreatedAt || updated.UpdatedAt < created.UpdatedAt {
		t.Fatalf("update failed: project=%+v err=%v", updated, err)
	}
	projects, err := catalogService.List(ctx, owner.Session.Token, owner.Workspaces[0].ID)
	if err != nil || len(projects) != 1 {
		t.Fatalf("list failed: projects=%+v err=%v", projects, err)
	}

	_, err = catalogService.List(ctx, other.Session.Token, owner.Workspaces[0].ID)
	assertCatalogCode(t, err, "workspace_forbidden")
	_, err = catalogService.Sync(ctx, other.Session.Token, other.Workspaces[0].ID, input)
	assertCatalogCode(t, err, "workspace_forbidden")

	projects, err = catalogService.List(ctx, owner.Session.Token, owner.Workspaces[0].ID)
	if err != nil || len(projects) != 1 || projects[0].Title != "任务看板" {
		t.Fatalf("original project changed: projects=%+v err=%v", projects, err)
	}
}

func TestProjectDocumentIsHashedIdempotentAndOptimistic(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	identityService := identity.NewService(NewIdentityRepository(database))
	catalogService := catalog.NewService(identityService, NewCatalogRepository(database))
	owner, _ := identityService.Register(ctx, identity.Registration{Email: "doc-owner@example.com", Password: "password-one", Name: "Owner", WorkspaceName: "Owner Space"})
	other, _ := identityService.Register(ctx, identity.Registration{Email: "doc-other@example.com", Password: "password-two", Name: "Other", WorkspaceName: "Other Space"})
	workspaceID, projectID, versionID := owner.Workspaces[0].ID, "project-doc", "version-doc"
	activeVersionID := versionID
	approvalID := "plan-approved"
	_, err = catalogService.Sync(ctx, owner.Session.Token, workspaceID, catalog.SyncInput{ID: projectID, Title: "云端项目", Status: "planning"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := documentSnapshot()
	trustedVersion := registerDocumentVersion(t, database, workspaceID, owner.User.ID, projectID, versionID, "", "构建任务板", "2026-01-01T00:00:00Z", snapshot)
	if _, err = database.Exec(`UPDATE workspace_projects SET active_version_id=?,status='ready' WHERE id=? AND workspace_id=?`, versionID, projectID, workspaceID); err != nil {
		t.Fatal(err)
	}
	payload := catalog.DocumentPayload{
		Project:  catalog.DocumentProject{ID: projectID, WorkspaceID: workspaceID, Title: "云端项目", Status: "ready", ApprovalID: &approvalID, ActiveVersionID: &activeVersionID, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
		Messages: []catalog.DocumentMessage{{ID: "message-1", ProjectID: projectID, Role: "user", Content: "构建任务板", CreatedAt: "2026-01-01T00:00:00Z"}},
		Versions: []catalog.DocumentVersion{trustedVersion},
	}
	first, err := catalogService.SaveDocument(ctx, owner.Session.Token, workspaceID, projectID, 0, payload)
	if err != nil || first.Revision != 1 || len(first.ContentHash) != 64 {
		t.Fatalf("first save: document=%+v err=%v", first, err)
	}
	retry, err := catalogService.SaveDocument(ctx, owner.Session.Token, workspaceID, projectID, 0, payload)
	if err != nil || retry.Revision != 1 || retry.ContentHash != first.ContentHash {
		t.Fatalf("idempotent retry: document=%+v err=%v", retry, err)
	}
	payload.Project.Title = "其他设备修改"
	_, err = catalogService.SaveDocument(ctx, owner.Session.Token, workspaceID, projectID, 0, payload)
	assertCatalogCode(t, err, "document_conflict")
	second, err := catalogService.SaveDocument(ctx, owner.Session.Token, workspaceID, projectID, 1, payload)
	if err != nil || second.Revision != 2 || second.ContentHash == first.ContentHash {
		t.Fatalf("second save: document=%+v err=%v", second, err)
	}
	tampered := payload
	tampered.Versions = append([]catalog.DocumentVersion(nil), payload.Versions...)
	tampered.Versions[0].Prompt = "改写已经发布的版本"
	_, err = catalogService.SaveDocument(ctx, owner.Session.Token, workspaceID, projectID, 2, tampered)
	assertCatalogCode(t, err, "immutable_version_violation")
	removed := payload
	removed.Versions = nil
	removed.Project.ActiveVersionID = nil
	_, err = catalogService.SaveDocument(ctx, owner.Session.Token, workspaceID, projectID, 2, removed)
	assertCatalogCode(t, err, "immutable_version_violation")
	duplicated := payload
	duplicated.Versions = append(append([]catalog.DocumentVersion(nil), payload.Versions...), payload.Versions[0])
	_, err = catalogService.SaveDocument(ctx, owner.Session.Token, workspaceID, projectID, 2, duplicated)
	assertCatalogCode(t, err, "invalid_document")
	fake := payload
	fake.Versions = append(append([]catalog.DocumentVersion(nil), payload.Versions...), catalog.DocumentVersion{ID: "version-fake", ProjectID: projectID, Prompt: "伪造版本", CreatedAt: "2026-01-02T00:00:00Z", Snapshot: snapshot})
	fake.Project.ActiveVersionID = &fake.Versions[1].ID
	_, err = catalogService.SaveDocument(ctx, owner.Session.Token, workspaceID, projectID, 2, fake)
	assertCatalogCode(t, err, "untrusted_version")
	trustedSecond := registerDocumentVersion(t, database, workspaceID, owner.User.ID, projectID, "version-doc-2", versionID, "增加筛选", "2026-01-02T00:00:00Z", snapshot)
	if _, err = database.Exec(`UPDATE workspace_projects SET active_version_id=? WHERE id=? AND workspace_id=?`, trustedSecond.ID, projectID, workspaceID); err != nil {
		t.Fatal(err)
	}
	payload.Versions = append(payload.Versions, trustedSecond)
	payload.Project.ActiveVersionID = &payload.Versions[1].ID
	third, err := catalogService.SaveDocument(ctx, owner.Session.Token, workspaceID, projectID, 2, payload)
	if err != nil || third.Revision != 3 || len(third.Payload.Versions) != 2 {
		t.Fatalf("append version: document=%+v err=%v", third, err)
	}
	restored, err := catalogService.GetDocument(ctx, owner.Session.Token, workspaceID, projectID)
	if err != nil || restored.Payload.Project.Title != "其他设备修改" || restored.Payload.Project.ApprovalID == nil || *restored.Payload.Project.ApprovalID != approvalID {
		t.Fatalf("restore: document=%+v err=%v", restored, err)
	}
	tamperedVersion := payload.Versions[1]
	tamperedVersion.Snapshot.Title = "Changed after release"
	tamperedJSON, _ := json.Marshal(tamperedVersion.Snapshot)
	if _, err = database.Exec(`UPDATE immutable_versions SET snapshot_json=? WHERE id=?`, string(tamperedJSON), tamperedVersion.ID); err != nil {
		t.Fatal(err)
	}
	tamperedVersions := append([]catalog.DocumentVersion(nil), payload.Versions...)
	tamperedVersions[1] = tamperedVersion
	matched, err := NewCatalogRepository(database).VersionsMatchForMember(ctx, owner.User.ID, workspaceID, projectID, tamperedVersions, payload.Project.ActiveVersionID)
	if err != nil || matched {
		t.Fatalf("tampered immutable version trusted: matched=%v err=%v", matched, err)
	}
	_, err = catalogService.GetDocument(ctx, other.Session.Token, workspaceID, projectID)
	assertCatalogCode(t, err, "workspace_forbidden")
	if err = catalogService.ResolveConflict(ctx, owner.Session.Token, workspaceID, projectID, "local"); err != nil {
		t.Fatal(err)
	}
	auditService := audit.NewService(identityService, NewAuditRepository(database))
	events, err := auditService.List(ctx, owner.Session.Token, workspaceID, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, event := range events {
		counts[event.Action]++
	}
	if counts["project.created"] != 1 || counts["project.document_saved"] != 3 || counts["project.conflict_resolved"] != 1 {
		t.Fatalf("unexpected audit counts: %+v", counts)
	}
	if counts["project.document_saved"] == 4 {
		t.Fatal("idempotent document retry must not create another audit event")
	}
}

func registerDocumentVersion(t *testing.T, database *sql.DB, workspaceID, accountID, projectID, versionID, parentID, prompt, createdAt string, snapshot domain.ProjectSnapshot) catalog.DocumentVersion {
	t.Helper()
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	candidateID := "candidate-" + versionID
	if _, err = database.Exec(`INSERT INTO build_candidates (id,workspace_id,account_id,project_id,usage_id,prompt,snapshot_json,snapshot_hash,status,created_at,committed_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, candidateID, workspaceID, accountID, projectID, "test", prompt, string(payload), hash, "committed", createdAt, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`INSERT INTO safety_verifications (candidate_id,snapshot_hash,policy,verified_at) VALUES (?,?,?,?)`, candidateID, hash, "snapshot-guard/v1", createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`INSERT INTO build_verifications (candidate_id,snapshot_hash,toolchain,duration_ms,verified_at) VALUES (?,?,?,?,?)`, candidateID, hash, "test-compiler", 10, createdAt); err != nil {
		t.Fatal(err)
	}
	var parent any
	var parentPointer *string
	if parentID != "" {
		parent = parentID
		parentPointer = &parentID
	}
	if _, err = database.Exec(`INSERT INTO immutable_versions (id,workspace_id,account_id,project_id,candidate_id,parent_version_id,prompt,snapshot_json,snapshot_hash,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, versionID, workspaceID, accountID, projectID, candidateID, parent, prompt, string(payload), hash, createdAt); err != nil {
		t.Fatal(err)
	}
	return catalog.DocumentVersion{ID: versionID, ProjectID: projectID, ParentVersionID: parentPointer, Prompt: prompt, Snapshot: snapshot, CreatedAt: createdAt, CandidateID: candidateID, SnapshotHash: hash, SourceAction: "restore", Build: &domain.BuildVerification{Toolchain: "test-compiler", DurationMS: 10, VerifiedAt: createdAt}, Safety: &domain.SafetyVerification{Policy: "snapshot-guard/v1", VerifiedAt: createdAt}}
}

func documentSnapshot() domain.ProjectSnapshot {
	return domain.ProjectSnapshot{
		Source: "template", Title: "Task", Summary: "Board", EntryFile: "/src/main.ts",
		Files:        map[string]string{"/src/main.ts": `import App from "./App.vue"`, "/src/App.vue": `<template><main>Task</main></template>`},
		Dependencies: map[string]string{"vue": "3.5.42"}, Backend: domain.BackendSpec{Auth: "none", Collections: []domain.BackendCollection{}},
	}
}

func assertCatalogCode(t *testing.T, err error, want string) {
	t.Helper()
	var catalogErr *catalog.Error
	if !errors.As(err, &catalogErr) || catalogErr.Code != want {
		t.Fatalf("expected catalog error %q, got %v", want, err)
	}
}
