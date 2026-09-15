package sqlite

import (
	"context"
	"testing"

	"vivatom-api-svc/internal/domain"
	runtimeservice "vivatom-api-svc/internal/runtime"
)

const (
	testPublicKey  = "public-key-123456789"
	testAdminToken = "admin-token-123456789"
)

func enabledBackend() domain.BackendSpec {
	return domain.BackendSpec{
		Enabled: true, Auth: "none",
		Collections: []domain.BackendCollection{{
			Name: "tasks", Label: "任务", Access: "public",
			Fields: []domain.BackendField{{
				Name: "title", Label: "标题", Type: "text", Required: true,
			}},
		}},
	}
}

func TestRuntimeProvisionLifecycle(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := runtimeservice.NewService(NewRuntimeRepository(database))
	input := runtimeservice.ProvisionInput{
		ProjectID: "00000000-0000-4000-8000-000000000001", Title: "任务板", Backend: enabledBackend(),
		PublicKey: testPublicKey, AdminToken: testAdminToken,
	}

	version, err := service.Provision(context.Background(), input)
	if err != nil || version != 1 {
		t.Fatalf("first provision: version=%d error=%v", version, err)
	}
	version, err = service.Provision(context.Background(), input)
	if err != nil || version != 1 {
		t.Fatalf("idempotent provision: version=%d error=%v", version, err)
	}

	input.Backend.Collections[0].Fields = append(
		input.Backend.Collections[0].Fields,
		domain.BackendField{Name: "note", Label: "备注", Type: "text"},
	)
	version, err = service.Provision(context.Background(), input)
	if err != nil || version != 2 {
		t.Fatalf("compatible update: version=%d error=%v", version, err)
	}
	inspection, err := service.Inspect(
		context.Background(), input.ProjectID, testPublicKey, testAdminToken,
	)
	if err != nil || inspection.SchemaVersion != 2 {
		t.Fatalf("inspect: %#v error=%v", inspection, err)
	}
}

func TestRuntimeRejectsCredentialAndSchemaChanges(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := runtimeservice.NewService(NewRuntimeRepository(database))
	input := runtimeservice.ProvisionInput{
		ProjectID: "00000000-0000-4000-8000-000000000001", Title: "任务板", Backend: enabledBackend(),
		PublicKey: testPublicKey, AdminToken: testAdminToken,
	}
	if _, err := service.Provision(context.Background(), input); err != nil {
		t.Fatal(err)
	}

	wrongCredentials := input
	wrongCredentials.AdminToken = "another-admin-token-123"
	if _, err := service.Provision(context.Background(), wrongCredentials); runtimeCode(err) != "forbidden" {
		t.Fatalf("wrong credential error = %v", err)
	}
	incompatible := input
	incompatible.Backend.Collections = nil
	if _, err := service.Provision(context.Background(), incompatible); runtimeCode(err) != "schema_conflict" {
		t.Fatalf("incompatible schema error = %v", err)
	}
}

func TestRuntimeAuthenticationLifecycleAndTenantIsolation(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := runtimeservice.NewService(NewRuntimeRepository(database))
	backend := enabledBackend()
	backend.Auth = "email_password"
	firstID := "00000000-0000-4000-8000-000000000001"
	secondID := "00000000-0000-4000-8000-000000000002"
	for _, projectID := range []string{firstID, secondID} {
		if _, err := service.Provision(context.Background(), runtimeservice.ProvisionInput{
			ProjectID: projectID, Title: "任务板", Backend: backend,
			PublicKey: testPublicKey + projectID, AdminToken: testAdminToken + projectID,
		}); err != nil {
			t.Fatal(err)
		}
	}

	authentication, err := service.Register(context.Background(), firstID, testPublicKey+firstID, " LEARNER@EXAMPLE.COM ", "correct-password")
	if err != nil || authentication.User.Email != "learner@example.com" || authentication.Token == "" {
		t.Fatalf("register: %#v error=%v", authentication, err)
	}
	var storedHash string
	if err := database.QueryRow(`SELECT password_hash FROM runtime_users WHERE project_id = ?`, firstID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == "correct-password" {
		t.Fatal("password was stored as plaintext")
	}
	if _, err := service.Login(context.Background(), firstID, testPublicKey+firstID, "learner@example.com", "wrong-password"); runtimeCode(err) != "invalid_credentials" {
		t.Fatalf("wrong password error = %v", err)
	}
	if _, err := service.Me(context.Background(), secondID, testPublicKey+secondID, authentication.Token); runtimeCode(err) != "unauthorized" {
		t.Fatalf("cross-project session error = %v", err)
	}
	if user, err := service.Me(context.Background(), firstID, testPublicKey+firstID, authentication.Token); err != nil || user.ID != authentication.User.ID {
		t.Fatalf("me: %#v error=%v", user, err)
	}
	if err := service.Logout(context.Background(), firstID, testPublicKey+firstID, authentication.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Me(context.Background(), firstID, testPublicKey+firstID, authentication.Token); runtimeCode(err) != "unauthorized" {
		t.Fatalf("logged out session error = %v", err)
	}
	if _, err := service.Login(context.Background(), firstID, testPublicKey+firstID, "learner@example.com", "correct-password"); err != nil {
		t.Fatalf("login after logout: %v", err)
	}
}

func TestRuntimeRecordCRUDAndAccessScopes(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := runtimeservice.NewService(NewRuntimeRepository(database))
	backend := domain.BackendSpec{Enabled: true, Auth: "email_password", Collections: []domain.BackendCollection{
		{Name: "private_tasks", Label: "私人任务", Access: "owner", Fields: []domain.BackendField{
			{Name: "title", Label: "标题", Type: "text", Required: true},
			{Name: "done", Label: "完成", Type: "boolean"},
		}},
		{Name: "announcements", Label: "公告", Access: "public", Fields: []domain.BackendField{
			{Name: "title", Label: "标题", Type: "text", Required: true},
			{Name: "published_at", Label: "发布时间", Type: "date"},
		}},
	}}
	firstID := "00000000-0000-4000-8000-000000000011"
	secondID := "00000000-0000-4000-8000-000000000012"
	for _, projectID := range []string{firstID, secondID} {
		if _, err := service.Provision(context.Background(), runtimeservice.ProvisionInput{
			ProjectID: projectID, Title: "记录测试", Backend: backend,
			PublicKey: testPublicKey + projectID, AdminToken: testAdminToken + projectID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	ownerA, err := service.Register(context.Background(), firstID, testPublicKey+firstID, "a@example.com", "password-a")
	if err != nil {
		t.Fatal(err)
	}
	ownerB, err := service.Register(context.Background(), firstID, testPublicKey+firstID, "b@example.com", "password-b")
	if err != nil {
		t.Fatal(err)
	}

	privateRecord, err := service.CreateRecord(context.Background(), firstID, testPublicKey+firstID, ownerA.Token, "private_tasks", map[string]any{"title": "学习隔离", "done": false})
	if err != nil {
		t.Fatal(err)
	}
	if records, err := service.ListRecords(context.Background(), firstID, testPublicKey+firstID, ownerB.Token, "private_tasks"); err != nil || len(records) != 0 {
		t.Fatalf("other owner list: %#v error=%v", records, err)
	}
	if _, err := service.UpdateRecord(context.Background(), firstID, testPublicKey+firstID, ownerB.Token, "private_tasks", privateRecord.ID, map[string]any{"done": true}); runtimeCode(err) != "not_found" {
		t.Fatalf("other owner update error = %v", err)
	}
	updated, err := service.UpdateRecord(context.Background(), firstID, testPublicKey+firstID, ownerA.Token, "private_tasks", privateRecord.ID, map[string]any{"done": true})
	if err != nil || updated.Data["title"] != "学习隔离" || updated.Data["done"] != true {
		t.Fatalf("owner update: %#v error=%v", updated, err)
	}

	publicRecord, err := service.CreateRecord(context.Background(), firstID, testPublicKey+firstID, "", "announcements", map[string]any{"title": "公开发布", "published_at": "2026-09-15T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if records, err := service.ListRecords(context.Background(), firstID, testPublicKey+firstID, "", "announcements"); err != nil || len(records) != 1 || records[0].ID != publicRecord.ID {
		t.Fatalf("public list: %#v error=%v", records, err)
	}
	if records, err := service.ListRecords(context.Background(), secondID, testPublicKey+secondID, "", "announcements"); err != nil || len(records) != 0 {
		t.Fatalf("cross-project list: %#v error=%v", records, err)
	}
	if err := service.DeleteRecord(context.Background(), firstID, testPublicKey+firstID, "", "announcements", publicRecord.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRecordValidation(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := runtimeservice.NewService(NewRuntimeRepository(database))
	projectID := "00000000-0000-4000-8000-000000000021"
	if _, err := service.Provision(context.Background(), runtimeservice.ProvisionInput{
		ProjectID: projectID, Title: "校验测试", Backend: enabledBackend(),
		PublicKey: testPublicKey, AdminToken: testAdminToken,
	}); err != nil {
		t.Fatal(err)
	}
	cases := []map[string]any{
		{},
		{"title": 42.0},
		{"title": "合法", "unknown": true},
		{"title": "合法", "id": "client-id"},
	}
	for _, input := range cases {
		if _, err := service.CreateRecord(context.Background(), projectID, testPublicKey, "", "tasks", input); runtimeCode(err) != "invalid_record" {
			t.Fatalf("input %#v error = %v", input, err)
		}
	}
	if _, err := service.CreateRecord(context.Background(), projectID, testPublicKey, "", "missing", map[string]any{"title": "合法"}); runtimeCode(err) != "not_found" {
		t.Fatalf("unknown collection error = %v", err)
	}
}

func runtimeCode(err error) string {
	if value, ok := err.(*runtimeservice.Error); ok {
		return value.Code
	}
	return ""
}
