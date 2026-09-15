package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"vivatom-api-svc/internal/domain"
	"vivatom-api-svc/internal/usage"
)

const defaultWorkspaceCreditLimit = 15

type UsageRepository struct{ db *sql.DB }

func NewUsageRepository(db *sql.DB) *UsageRepository { return &UsageRepository{db: db} }

func (r *UsageRepository) StorePlan(ctx context.Context, workspaceID, accountID, projectID string, plan domain.BuildPlan, createdAt string) (string, error) {
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	var id string
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO approved_plans (id,workspace_id,account_id,project_id,plan_json,status,created_at)
		SELECT 'plan_'||lower(hex(randomblob(16))),?,?,?,?, 'pending', ?
		WHERE EXISTS (SELECT 1 FROM memberships WHERE workspace_id=? AND account_id=?)
		RETURNING id`, workspaceID, accountID, projectID, string(payload), createdAt, workspaceID, accountID).Scan(&id)
	return id, err
}

func (r *UsageRepository) ApprovePlan(ctx context.Context, accountID, workspaceID, projectID, approvalID, approvedAt string) (usage.Result, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var member int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, workspaceID).Scan(&member); err != nil {
		return "", err
	}
	if member == 0 {
		return usage.ResultForbidden, nil
	}
	result, err := tx.ExecContext(ctx, `UPDATE approved_plans SET status='approved',approved_at=? WHERE id=? AND workspace_id=? AND account_id=? AND project_id=? AND status='pending'`, approvedAt, approvalID, workspaceID, accountID, projectID)
	if err != nil {
		return "", err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if changed != 1 {
		return usage.ResultApprovalInvalid, nil
	}
	if err = appendAudit(ctx, tx, workspaceID, accountID, "plan.approved", "project", projectID, approvedAt, map[string]any{"approvalId": approvalID}); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return usage.ResultOK, nil
}

func (r *UsageRepository) ReserveApproved(ctx context.Context, accountID, workspaceID, projectID, approvalID string, credits int, createdAt string) (string, *domain.BuildPlan, usage.Result, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, "", err
	}
	defer tx.Rollback()
	var member int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, workspaceID).Scan(&member); err != nil {
		return "", nil, "", err
	}
	if member == 0 {
		return "", nil, usage.ResultForbidden, nil
	}
	var used int
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(sum(credits),0) FROM agent_usage WHERE workspace_id=?`, workspaceID).Scan(&used); err != nil {
		return "", nil, "", err
	}
	if used+credits > defaultWorkspaceCreditLimit {
		return "", nil, usage.ResultExhausted, nil
	}
	var payload string
	err = tx.QueryRowContext(ctx, `SELECT plan_json FROM approved_plans WHERE id=? AND workspace_id=? AND account_id=? AND project_id=? AND status='approved'`, approvalID, workspaceID, accountID, projectID).Scan(&payload)
	if err == sql.ErrNoRows {
		return "", nil, usage.ResultApprovalInvalid, nil
	}
	if err != nil {
		return "", nil, "", err
	}
	var plan domain.BuildPlan
	if err = json.Unmarshal([]byte(payload), &plan); err != nil {
		return "", nil, "", err
	}
	if err = domain.ValidateBuildPlan(plan); err != nil {
		return "", nil, "", err
	}
	result, err := tx.ExecContext(ctx, `UPDATE approved_plans SET status='consumed',consumed_at=? WHERE id=? AND status='approved'`, createdAt, approvalID)
	if err != nil {
		return "", nil, "", err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return "", nil, "", err
	}
	if changed != 1 {
		return "", nil, usage.ResultApprovalInvalid, nil
	}
	var id string
	if err = tx.QueryRowContext(ctx, `INSERT INTO agent_usage (id,workspace_id,account_id,project_id,action,credits,status,created_at) VALUES (lower(hex(randomblob(16))),?,?,?,'build',?,'running',?) RETURNING id`, workspaceID, accountID, projectID, credits, createdAt).Scan(&id); err != nil {
		return "", nil, "", err
	}
	if err = appendAudit(ctx, tx, workspaceID, accountID, "agent.started", "project", projectID, createdAt, map[string]any{"action": "build", "credits": credits, "approvalId": approvalID}); err != nil {
		return "", nil, "", err
	}
	if err = tx.Commit(); err != nil {
		return "", nil, "", err
	}
	return id, &plan, usage.ResultOK, nil
}

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
