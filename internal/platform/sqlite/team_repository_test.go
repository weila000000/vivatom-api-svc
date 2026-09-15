package sqlite

import (
	"context"
	"errors"
	"testing"

	"vivatom-api-svc/internal/audit"
	"vivatom-api-svc/internal/identity"
	"vivatom-api-svc/internal/team"
)

func TestTeamInvitationRolesAndLastOwnerProtection(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	identityService := identity.NewService(NewIdentityRepository(database))
	teamService := team.NewService(identityService, NewTeamRepository(database))
	owner, _ := identityService.Register(ctx, identity.Registration{Email: "owner@team.test", Password: "password-one", Name: "Owner", WorkspaceName: "Team"})
	member, _ := identityService.Register(ctx, identity.Registration{Email: "member@team.test", Password: "password-two", Name: "Member", WorkspaceName: "Personal"})
	wrong, _ := identityService.Register(ctx, identity.Registration{Email: "wrong@team.test", Password: "password-three", Name: "Wrong", WorkspaceName: "Wrong"})
	workspaceID := owner.Workspaces[0].ID

	invitation, err := teamService.Invite(ctx, owner.Session.Token, workspaceID, "member@team.test", "member")
	if err != nil || invitation.Token == "" {
		t.Fatalf("invite: %+v err=%v", invitation, err)
	}
	var storedToken string
	if err := database.QueryRow(`SELECT token_hash FROM workspace_invitations WHERE id=?`, invitation.ID).Scan(&storedToken); err != nil {
		t.Fatal(err)
	}
	if storedToken == invitation.Token || len(storedToken) != 64 {
		t.Fatal("invitation token must only be stored as a SHA-256 digest")
	}
	_, err = teamService.Accept(ctx, wrong.Session.Token, invitation.Token)
	assertTeamCode(t, err, "invitation_email_mismatch")
	accepted, err := teamService.Accept(ctx, member.Session.Token, invitation.Token)
	if err != nil || accepted.Role != "member" {
		t.Fatalf("accept: %+v err=%v", accepted, err)
	}
	_, err = teamService.Accept(ctx, member.Session.Token, invitation.Token)
	assertTeamCode(t, err, "invitation_invalid")
	members, err := teamService.List(ctx, member.Session.Token, workspaceID)
	if err != nil || len(members) != 2 {
		t.Fatalf("list: %+v err=%v", members, err)
	}

	_, err = teamService.Invite(ctx, member.Session.Token, workspaceID, "another@team.test", "member")
	assertTeamCode(t, err, "owner_required")
	if err = teamService.ChangeRole(ctx, owner.Session.Token, workspaceID, member.User.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if err = teamService.ChangeRole(ctx, member.Session.Token, workspaceID, owner.User.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if err = teamService.Remove(ctx, member.Session.Token, workspaceID, owner.User.ID); err != nil {
		t.Fatal(err)
	}
	if err = teamService.ChangeRole(ctx, member.Session.Token, workspaceID, member.User.ID, "member"); err == nil {
		t.Fatal("last owner demotion should fail")
	} else {
		assertTeamCode(t, err, "last_owner")
	}
	if err = teamService.Remove(ctx, member.Session.Token, workspaceID, member.User.ID); err == nil {
		t.Fatal("last owner removal should fail")
	} else {
		assertTeamCode(t, err, "last_owner")
	}
	auditService := audit.NewService(identityService, NewAuditRepository(database))
	events, err := auditService.List(ctx, member.Session.Token, workspaceID, "", 20)
	if err != nil || len(events) != 5 {
		t.Fatalf("audit list: events=%+v err=%v", events, err)
	}
	want := []string{"member.removed", "member.role_changed", "member.role_changed", "member.invitation_accepted", "member.invited"}
	for index, action := range want {
		if events[index].Action != action {
			t.Fatalf("audit event %d: want %s, got %s", index, action, events[index].Action)
		}
		if string(events[index].Metadata) == "" || string(events[index].Metadata) == "null" {
			t.Fatalf("audit event %d has no metadata", index)
		}
	}
	if _, err = auditService.List(ctx, wrong.Session.Token, workspaceID, "", 20); err == nil {
		t.Fatal("non-member should not read workspace audit")
	}
}

func assertTeamCode(t *testing.T, err error, want string) {
	t.Helper()
	var value *team.Error
	if !errors.As(err, &value) || value.Code != want {
		t.Fatalf("expected team error %q, got %v", want, err)
	}
}
