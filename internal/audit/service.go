package audit

import (
	"context"
	"net/http"
	"time"

	"vivatom-api-svc/internal/identity"
)

type Identity interface {
	Me(context.Context, string) (identity.Account, []identity.Workspace, error)
}
type Repository interface {
	List(context.Context, string, string, string, int) ([]Event, bool, error)
}
type Service struct {
	identity   Identity
	repository Repository
}

func NewService(identityService Identity, repository Repository) *Service {
	return &Service{identity: identityService, repository: repository}
}

func (s *Service) List(ctx context.Context, token, workspaceID, before string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if before != "" {
		if _, err := time.Parse(time.RFC3339Nano, before); err != nil {
			return nil, &Error{Code: "invalid_cursor", Status: http.StatusBadRequest}
		}
	}
	if s.identity == nil || s.repository == nil {
		return nil, &Error{Code: "audit_unavailable", Status: http.StatusServiceUnavailable}
	}
	account, _, err := s.identity.Me(ctx, token)
	if err != nil {
		return nil, &Error{Code: "unauthorized", Status: http.StatusUnauthorized}
	}
	events, allowed, err := s.repository.List(ctx, account.ID, workspaceID, before, limit)
	if err != nil {
		return nil, &Error{Code: "audit_unavailable", Status: http.StatusServiceUnavailable}
	}
	if !allowed {
		return nil, &Error{Code: "workspace_forbidden", Status: http.StatusForbidden}
	}
	return events, nil
}
