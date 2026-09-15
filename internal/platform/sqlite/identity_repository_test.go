package sqlite

import (
	"context"
	"errors"
	"testing"

	"vivatom-api-svc/internal/identity"
)

func TestIdentityRegistrationAndSessionLifecycle(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	service := identity.NewService(NewIdentityRepository(database))
	ctx := context.Background()
	input := identity.Registration{
		Email:         " Learner@Vivatom.dev ",
		Password:      "correct-horse",
		Name:          "学习者",
		WorkspaceName: "第一个工作区",
	}
	auth, err := service.Register(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if auth.User.Email != "learner@vivatom.dev" || auth.Session.Token == "" {
		t.Fatalf("unexpected authentication: %+v", auth)
	}
	if len(auth.Workspaces) != 1 || auth.Workspaces[0].Role != "owner" {
		t.Fatalf("unexpected workspaces: %+v", auth.Workspaces)
	}

	var storedHash string
	if err := database.QueryRow(`SELECT password_hash FROM tenant_accounts WHERE id=?`, auth.User.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == input.Password || storedHash == "" {
		t.Fatal("password must be stored as a non-empty digest")
	}

	_, err = service.Register(ctx, input)
	assertIdentityCode(t, err, "email_taken")
	for _, table := range []string{"tenant_accounts", "workspaces", "memberships"} {
		var count int
		if err := database.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("duplicate registration left %d rows in %s", count, table)
		}
	}

	_, err = service.Login(ctx, input.Email, "wrong-password")
	assertIdentityCode(t, err, "invalid_credentials")
	loggedIn, err := service.Login(ctx, input.Email, input.Password)
	if err != nil {
		t.Fatal(err)
	}
	account, workspaces, err := service.Me(ctx, loggedIn.Session.Token)
	if err != nil || account.ID != auth.User.ID || len(workspaces) != 1 {
		t.Fatalf("unexpected current identity: account=%+v workspaces=%+v err=%v", account, workspaces, err)
	}
	if err := service.Logout(ctx, loggedIn.Session.Token); err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Me(ctx, loggedIn.Session.Token)
	assertIdentityCode(t, err, "unauthorized")
}

func assertIdentityCode(t *testing.T, err error, want string) {
	t.Helper()
	var identityErr *identity.Error
	if !errors.As(err, &identityErr) || identityErr.Code != want {
		t.Fatalf("expected identity error %q, got %v", want, err)
	}
}
