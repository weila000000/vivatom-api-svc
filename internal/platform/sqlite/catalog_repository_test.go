package sqlite

import (
	"context"
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
	_, err = catalogService.Sync(ctx, owner.Session.Token, workspaceID, catalog.SyncInput{ID: projectID, Title: "云端项目", Status: "ready", ActiveVersionID: &activeVersionID})
	if err != nil {
		t.Fatal(err)
	}
	payload := catalog.DocumentPayload{
		Project:  catalog.DocumentProject{ID: projectID, WorkspaceID: workspaceID, Title: "云端项目", Status: "ready", ActiveVersionID: &activeVersionID, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
		Messages: []catalog.DocumentMessage{{ID: "message-1", ProjectID: projectID, Role: "user", Content: "构建任务板", CreatedAt: "2026-01-01T00:00:00Z"}},
		Versions: []catalog.DocumentVersion{{ID: versionID, ProjectID: projectID, Prompt: "构建任务板", CreatedAt: "2026-01-01T00:00:00Z", Snapshot: documentSnapshot()}},
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
	restored, err := catalogService.GetDocument(ctx, owner.Session.Token, workspaceID, projectID)
	if err != nil || restored.Payload.Project.Title != "其他设备修改" {
		t.Fatalf("restore: document=%+v err=%v", restored, err)
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
	if counts["project.created"] != 1 || counts["project.document_saved"] != 2 || counts["project.conflict_resolved"] != 1 {
		t.Fatalf("unexpected audit counts: %+v", counts)
	}
	if counts["project.document_saved"] == 3 {
		t.Fatal("idempotent document retry must not create another audit event")
	}
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
