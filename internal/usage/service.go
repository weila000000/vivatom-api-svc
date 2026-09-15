package usage

import (
	"context"
	"net/http"
	"time"

	"vivatom-api-svc/internal/domain"
	"vivatom-api-svc/internal/identity"
)

type Identity interface {
	Me(context.Context, string) (identity.Account, []identity.Workspace, error)
}
type Runner interface {
	Run(context.Context, domain.AgentRequest) (<-chan domain.AgentEvent, error)
}
type Repository interface {
	Reserve(context.Context, string, string, string, string, int, string) (string, Result, error)
	Complete(context.Context, string, string, string) error
	Summary(context.Context, string, string) (Summary, bool, error)
}

type Service struct {
	identity   Identity
	runner     Runner
	repository Repository
	now        func() time.Time
}

func NewService(identityService Identity, runner Runner, repository Repository) *Service {
	return &Service{identity: identityService, runner: runner, repository: repository, now: time.Now}
}

func (s *Service) Run(ctx context.Context, token, workspaceID string, request domain.AgentRequest) (<-chan domain.AgentEvent, error) {
	account, err := s.account(ctx, token)
	if err != nil {
		return nil, err
	}
	cost := actionCost(request.Action)
	if cost == 0 || request.Validate() != nil {
		return nil, &Error{Code: "invalid_request", Status: http.StatusBadRequest}
	}
	usageID, result, err := s.repository.Reserve(ctx, account.ID, workspaceID, request.ProjectID, string(request.Action), cost, s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, unavailable()
	}
	if result == ResultForbidden {
		return nil, &Error{Code: "workspace_forbidden", Status: http.StatusForbidden}
	}
	if result == ResultExhausted {
		return nil, &Error{Code: "quota_exhausted", Status: http.StatusPaymentRequired}
	}
	events, err := s.runner.Run(ctx, request)
	if err != nil {
		_ = s.repository.Complete(context.Background(), usageID, "failed", s.now().UTC().Format(time.RFC3339Nano))
		return nil, err
	}
	output := make(chan domain.AgentEvent)
	go func() {
		defer close(output)
		status := "succeeded"
		for event := range events {
			if event.Type == "error" {
				status = "failed"
			}
			select {
			case output <- event:
			case <-ctx.Done():
				status = "cancelled"
				_ = s.repository.Complete(context.Background(), usageID, status, s.now().UTC().Format(time.RFC3339Nano))
				return
			}
		}
		_ = s.repository.Complete(context.Background(), usageID, status, s.now().UTC().Format(time.RFC3339Nano))
	}()
	return output, nil
}

func (s *Service) Summary(ctx context.Context, token, workspaceID string) (Summary, error) {
	account, err := s.account(ctx, token)
	if err != nil {
		return Summary{}, err
	}
	summary, allowed, err := s.repository.Summary(ctx, account.ID, workspaceID)
	if err != nil {
		return Summary{}, unavailable()
	}
	if !allowed {
		return Summary{}, &Error{Code: "workspace_forbidden", Status: http.StatusForbidden}
	}
	return summary, nil
}

func (s *Service) account(ctx context.Context, token string) (identity.Account, error) {
	if s.identity == nil || s.repository == nil || s.runner == nil {
		return identity.Account{}, unavailable()
	}
	account, _, err := s.identity.Me(ctx, token)
	if err != nil {
		return identity.Account{}, &Error{Code: "unauthorized", Status: http.StatusUnauthorized}
	}
	return account, nil
}
func actionCost(action domain.AgentAction) int {
	if action == domain.ActionPlan {
		return 1
	}
	if action == domain.ActionBuild || action == domain.ActionIterate || action == domain.ActionRepair || action == domain.ActionPolish {
		return 4
	}
	return 0
}
func unavailable() error {
	return &Error{Code: "usage_unavailable", Status: http.StatusServiceUnavailable}
}
