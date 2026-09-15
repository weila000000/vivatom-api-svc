package team

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"vivatom-api-svc/internal/identity"
)

type Identity interface {
	Me(context.Context, string) (identity.Account, []identity.Workspace, error)
}
type Repository interface {
	ListMembers(context.Context, string, string) ([]Member, bool, error)
	CreateInvitation(context.Context, string, StoredInvitation) (bool, error)
	AcceptInvitation(context.Context, string, string, string, string) (*Member, Result, error)
	ChangeRole(context.Context, string, string, string, string, string) (Result, error)
	RemoveMember(context.Context, string, string, string, string) (Result, error)
}

type Service struct {
	identity   Identity
	repository Repository
	now        func() time.Time
}

func NewService(identityService Identity, repository Repository) *Service {
	return &Service{identity: identityService, repository: repository, now: time.Now}
}

func (s *Service) List(ctx context.Context, token, workspaceID string) ([]Member, error) {
	account, err := s.account(ctx, token)
	if err != nil {
		return nil, err
	}
	members, allowed, err := s.repository.ListMembers(ctx, account.ID, workspaceID)
	if err != nil {
		return nil, unavailable()
	}
	if !allowed {
		return nil, apiError("workspace_forbidden", http.StatusForbidden)
	}
	return members, nil
}

func (s *Service) Invite(ctx context.Context, token, workspaceID, email, role string) (Invitation, error) {
	account, err := s.account(ctx, token)
	if err != nil {
		return Invitation{}, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !validEmail(email) || !validRole(role) {
		return Invitation{}, apiError("invalid_invitation", http.StatusBadRequest)
	}
	rawToken, err := randomValue(32)
	if err != nil {
		return Invitation{}, unavailable()
	}
	now := s.now().UTC()
	id, err := randomValue(12)
	if err != nil {
		return Invitation{}, unavailable()
	}
	invitation := Invitation{ID: "invite_" + id, WorkspaceID: workspaceID, Email: email, Role: role, Token: rawToken, ExpiresAt: now.Add(7 * 24 * time.Hour).Format(time.RFC3339Nano), CreatedAt: now.Format(time.RFC3339Nano)}
	allowed, err := s.repository.CreateInvitation(ctx, account.ID, StoredInvitation{Invitation: invitation, TokenHash: tokenHash(rawToken), InvitedBy: account.ID})
	if err != nil {
		return Invitation{}, unavailable()
	}
	if !allowed {
		return Invitation{}, apiError("owner_required", http.StatusForbidden)
	}
	return invitation, nil
}

func (s *Service) Accept(ctx context.Context, token, invitationToken string) (Member, error) {
	account, err := s.account(ctx, token)
	if err != nil {
		return Member{}, err
	}
	if strings.TrimSpace(invitationToken) == "" {
		return Member{}, apiError("invalid_invitation", http.StatusBadRequest)
	}
	member, result, err := s.repository.AcceptInvitation(ctx, account.ID, account.Email, tokenHash(invitationToken), s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return Member{}, unavailable()
	}
	if result == ResultEmailMismatch {
		return Member{}, apiError("invitation_email_mismatch", http.StatusForbidden)
	}
	if result != ResultOK || member == nil {
		return Member{}, apiError("invitation_invalid", http.StatusGone)
	}
	return *member, nil
}

func (s *Service) ChangeRole(ctx context.Context, token, workspaceID, accountID, role string) error {
	actor, err := s.account(ctx, token)
	if err != nil {
		return err
	}
	if !validRole(role) {
		return apiError("invalid_role", http.StatusBadRequest)
	}
	result, err := s.repository.ChangeRole(ctx, actor.ID, workspaceID, accountID, role, s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return unavailable()
	}
	return resultError(result)
}

func (s *Service) Remove(ctx context.Context, token, workspaceID, accountID string) error {
	actor, err := s.account(ctx, token)
	if err != nil {
		return err
	}
	result, err := s.repository.RemoveMember(ctx, actor.ID, workspaceID, accountID, s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return unavailable()
	}
	return resultError(result)
}

func (s *Service) account(ctx context.Context, token string) (identity.Account, error) {
	if s.identity == nil {
		return identity.Account{}, unavailable()
	}
	account, _, err := s.identity.Me(ctx, token)
	if err != nil {
		return identity.Account{}, apiError("unauthorized", http.StatusUnauthorized)
	}
	return account, nil
}

func resultError(result Result) error {
	switch result {
	case ResultOK:
		return nil
	case ResultLastOwner:
		return apiError("last_owner", http.StatusConflict)
	case ResultNotFound:
		return apiError("member_not_found", http.StatusNotFound)
	default:
		return apiError("owner_required", http.StatusForbidden)
	}
}
func validRole(role string) bool { return role == "owner" || role == "member" }
func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && len(value) <= 254
}
func randomValue(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func apiError(code string, status int) error { return &Error{Code: code, Status: status} }
func unavailable() error                     { return apiError("team_unavailable", http.StatusServiceUnavailable) }
