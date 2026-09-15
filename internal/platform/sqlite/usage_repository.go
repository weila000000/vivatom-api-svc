package sqlite

import (
	"context"
	"database/sql"

	"vivatom-api-svc/internal/usage"
)

const defaultWorkspaceCreditLimit = 15

type UsageRepository struct{ db *sql.DB }

func NewUsageRepository(db *sql.DB) *UsageRepository { return &UsageRepository{db: db} }

func (r *UsageRepository) Reserve(ctx context.Context, accountID, workspaceID, projectID, action string, credits int, createdAt string) (string, usage.Result, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	var member int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, workspaceID).Scan(&member); err != nil {
		return "", "", err
	}
	if member == 0 {
		return "", usage.ResultForbidden, nil
	}
	var used int
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(sum(credits),0) FROM agent_usage WHERE workspace_id=?`, workspaceID).Scan(&used); err != nil {
		return "", "", err
	}
	if used+credits > defaultWorkspaceCreditLimit {
		return "", usage.ResultExhausted, nil
	}
	var id string
	if err = tx.QueryRowContext(ctx, `INSERT INTO agent_usage (id,workspace_id,account_id,project_id,action,credits,status,created_at) VALUES (lower(hex(randomblob(16))),?,?,?,?,?,'running',?) RETURNING id`, workspaceID, accountID, projectID, action, credits, createdAt).Scan(&id); err != nil {
		return "", "", err
	}
	if err = appendAudit(ctx, tx, workspaceID, accountID, "agent.started", "project", projectID, createdAt, map[string]any{"action": action, "credits": credits}); err != nil {
		return "", "", err
	}
	if err = tx.Commit(); err != nil {
		return "", "", err
	}
	return id, usage.ResultOK, nil
}

func (r *UsageRepository) Complete(ctx context.Context, id, status, completedAt string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE agent_usage SET status=?,completed_at=? WHERE id=? AND status='running'`, status, completedAt, id)
	return err
}

func (r *UsageRepository) Summary(ctx context.Context, accountID, workspaceID string) (usage.Summary, bool, error) {
	var member int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, workspaceID).Scan(&member); err != nil {
		return usage.Summary{}, false, err
	}
	if member == 0 {
		return usage.Summary{}, false, nil
	}
	var used int
	if err := r.db.QueryRowContext(ctx, `SELECT coalesce(sum(credits),0) FROM agent_usage WHERE workspace_id=?`, workspaceID).Scan(&used); err != nil {
		return usage.Summary{}, false, err
	}
	remaining := defaultWorkspaceCreditLimit - used
	if remaining < 0 {
		remaining = 0
	}
	return usage.Summary{WorkspaceID: workspaceID, Limit: defaultWorkspaceCreditLimit, Used: used, Remaining: remaining}, true, nil
}
