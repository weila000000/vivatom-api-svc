package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"vivatom-api-svc/internal/domain"
	"vivatom-api-svc/internal/generation"
	"vivatom-api-svc/internal/usage"
)

const defaultWorkspaceCreditLimit = 15

type UsageRepository struct{ db *sql.DB }

func NewUsageRepository(db *sql.DB) *UsageRepository { return &UsageRepository{db: db} }

func (r *UsageRepository) StoreCandidate(ctx context.Context, workspaceID, accountID, projectID, usageID, prompt string, snapshot domain.ProjectSnapshot, createdAt string) (string, string, error) {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(payload)
	snapshotHash := hex.EncodeToString(sum[:])
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO build_candidates (id,workspace_id,account_id,project_id,usage_id,prompt,snapshot_json,snapshot_hash,status,created_at)
		SELECT 'candidate_'||lower(hex(randomblob(16))),?,?,?,?,?,?,?,'pending',?
		WHERE EXISTS (SELECT 1 FROM agent_usage WHERE id=? AND workspace_id=? AND account_id=? AND project_id=?)
		RETURNING id`, workspaceID, accountID, projectID, usageID, prompt, string(payload), snapshotHash, createdAt, usageID, workspaceID, accountID, projectID).Scan(&id)
	if err != nil {
		return "", "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO safety_verifications (candidate_id,snapshot_hash,policy,verified_at) VALUES (?,?,?,?)`, id, snapshotHash, generation.PolicyVersion, createdAt); err != nil {
		return "", "", err
	}
	if err = tx.Commit(); err != nil {
		return "", "", err
	}
	return id, snapshotHash, nil
}

func (r *UsageRepository) LoadCandidate(ctx context.Context, accountID, workspaceID, projectID, candidateID, snapshotHash, prompt string) (*domain.ProjectSnapshot, usage.Result, error) {
	var member int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, workspaceID).Scan(&member); err != nil {
		return nil, "", err
	}
	if member == 0 {
		return nil, usage.ResultForbidden, nil
	}
	var payload string
	err := r.db.QueryRowContext(ctx, `SELECT snapshot_json FROM build_candidates WHERE id=? AND workspace_id=? AND account_id=? AND project_id=? AND snapshot_hash=? AND prompt=? AND status IN ('pending','committed')`, candidateID, workspaceID, accountID, projectID, snapshotHash, prompt).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, usage.ResultCandidateInvalid, nil
	}
	if err != nil {
		return nil, "", err
	}
	var snapshot domain.ProjectSnapshot
	if err = json.Unmarshal([]byte(payload), &snapshot); err != nil {
		return nil, "", err
	}
	return &snapshot, usage.ResultOK, nil
}

func (r *UsageRepository) RecordVerification(ctx context.Context, accountID, workspaceID, projectID, candidateID, snapshotHash string, verification domain.BuildVerification, verifiedAt string) (*domain.BuildVerification, usage.Result, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO build_verifications (candidate_id,snapshot_hash,toolchain,duration_ms,verified_at)
		SELECT id,?,?,?,? FROM build_candidates
		WHERE id=? AND workspace_id=? AND account_id=? AND project_id=? AND snapshot_hash=? AND status IN ('pending','committed')
		ON CONFLICT(candidate_id) DO NOTHING`, snapshotHash, verification.Toolchain, verification.DurationMS, verifiedAt, candidateID, workspaceID, accountID, projectID, snapshotHash)
	if err != nil {
		return nil, "", err
	}
	if _, err = result.RowsAffected(); err != nil {
		return nil, "", err
	}
	var stored domain.BuildVerification
	err = r.db.QueryRowContext(ctx, `SELECT toolchain,duration_ms,verified_at FROM build_verifications WHERE candidate_id=? AND snapshot_hash=?`, candidateID, snapshotHash).Scan(&stored.Toolchain, &stored.DurationMS, &stored.VerifiedAt)
	if err == sql.ErrNoRows {
		return nil, usage.ResultCandidateInvalid, nil
	}
	if err != nil {
		return nil, "", err
	}
	return &stored, usage.ResultOK, nil
}

func (r *UsageRepository) RestageVersion(ctx context.Context, accountID, workspaceID, projectID, versionID, createdAt string) (*usage.Candidate, usage.Result, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	var member int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, workspaceID).Scan(&member); err != nil {
		return nil, "", err
	}
	if member == 0 {
		return nil, usage.ResultForbidden, nil
	}
	var snapshotJSON, snapshotHash, safetyPolicy, safetyVerifiedAt string
	if err = tx.QueryRowContext(ctx, `SELECT v.snapshot_json,v.snapshot_hash,s.policy,s.verified_at FROM immutable_versions v JOIN build_verifications b ON b.candidate_id=v.candidate_id AND b.snapshot_hash=v.snapshot_hash JOIN safety_verifications s ON s.candidate_id=v.candidate_id AND s.snapshot_hash=v.snapshot_hash WHERE v.id=? AND v.workspace_id=? AND v.project_id=?`, versionID, workspaceID, projectID).Scan(&snapshotJSON, &snapshotHash, &safetyPolicy, &safetyVerifiedAt); err == sql.ErrNoRows {
		return nil, usage.ResultCandidateInvalid, nil
	} else if err != nil {
		return nil, "", err
	}
	var snapshot domain.ProjectSnapshot
	if err = json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		return nil, "", err
	}
	prompt := "恢复历史版本：" + snapshot.Title
	var candidateID string
	if err = tx.QueryRowContext(ctx, `INSERT INTO build_candidates (id,workspace_id,account_id,project_id,usage_id,prompt,snapshot_json,snapshot_hash,status,created_at) VALUES ('candidate_'||lower(hex(randomblob(16))),?,?,?,?,?,?,?, 'pending',?) RETURNING id`, workspaceID, accountID, projectID, "restore:"+versionID, prompt, snapshotJSON, snapshotHash, createdAt).Scan(&candidateID); err != nil {
		return nil, "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO safety_verifications (candidate_id,snapshot_hash,policy,verified_at) VALUES (?,?,?,?)`, candidateID, snapshotHash, safetyPolicy, safetyVerifiedAt); err != nil {
		return nil, "", err
	}
	if err = appendAudit(ctx, tx, workspaceID, accountID, "version.restaged", "project", projectID, createdAt, map[string]any{"sourceVersionId": versionID, "candidateId": candidateID}); err != nil {
		return nil, "", err
	}
	if err = tx.Commit(); err != nil {
		return nil, "", err
	}
	return &usage.Candidate{ID: candidateID, SnapshotHash: snapshotHash, Prompt: prompt, Snapshot: snapshot}, usage.ResultOK, nil
}

func (r *UsageRepository) CommitCandidate(ctx context.Context, accountID, workspaceID, projectID, candidateID, snapshotHash, parentVersionID, prompt, createdAt string) (*usage.Version, usage.Result, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	var member int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, workspaceID).Scan(&member); err != nil {
		return nil, "", err
	}
	if member == 0 {
		return nil, usage.ResultForbidden, nil
	}
	var snapshotJSON, storedHash, sourceAction, approvalID, safetyPolicy, safetyVerifiedAt string
	err = tx.QueryRowContext(ctx, `SELECT c.snapshot_json,c.snapshot_hash,coalesce(u.action,'restore'),coalesce(u.approval_id,''),s.policy,s.verified_at FROM build_candidates c JOIN build_verifications v ON v.candidate_id=c.id AND v.snapshot_hash=c.snapshot_hash JOIN safety_verifications s ON s.candidate_id=c.id AND s.snapshot_hash=c.snapshot_hash LEFT JOIN agent_usage u ON u.id=c.usage_id WHERE c.id=? AND c.workspace_id=? AND c.account_id=? AND c.project_id=? AND c.status='pending'`, candidateID, workspaceID, accountID, projectID).Scan(&snapshotJSON, &storedHash, &sourceAction, &approvalID, &safetyPolicy, &safetyVerifiedAt)
	if err == sql.ErrNoRows {
		var existing usage.Version
		err = tx.QueryRowContext(ctx, `SELECT v.id,v.project_id,coalesce(v.parent_version_id,''),v.prompt,v.snapshot_json,v.candidate_id,v.snapshot_hash,v.created_at,coalesce(u.action,'restore'),coalesce(u.approval_id,''),s.policy,s.verified_at FROM immutable_versions v JOIN build_candidates c ON c.id=v.candidate_id JOIN safety_verifications s ON s.candidate_id=c.id AND s.snapshot_hash=v.snapshot_hash LEFT JOIN agent_usage u ON u.id=c.usage_id WHERE v.candidate_id=? AND v.workspace_id=? AND v.account_id=? AND v.project_id=? AND v.snapshot_hash=? AND coalesce(v.parent_version_id,'')=? AND v.prompt=?`, candidateID, workspaceID, accountID, projectID, snapshotHash, parentVersionID, prompt).Scan(&existing.ID, &existing.ProjectID, &existing.ParentVersionID, &existing.Prompt, &snapshotJSON, &existing.CandidateID, &existing.SnapshotHash, &existing.CreatedAt, &existing.SourceAction, &existing.ApprovalID, &safetyPolicy, &safetyVerifiedAt)
		if err == sql.ErrNoRows {
			return nil, usage.ResultCandidateInvalid, nil
		}
		if err != nil {
			return nil, "", err
		}
		if err = json.Unmarshal([]byte(snapshotJSON), &existing.Snapshot); err != nil {
			return nil, "", err
		}
		existing.Safety = &domain.SafetyVerification{Policy: safetyPolicy, VerifiedAt: safetyVerifiedAt}
		return &existing, usage.ResultOK, nil
	}
	if err != nil {
		return nil, "", err
	}
	if storedHash != snapshotHash {
		return nil, usage.ResultCandidateInvalid, nil
	}
	var activeVersion sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT active_version_id FROM workspace_projects WHERE id=? AND workspace_id=?`, projectID, workspaceID).Scan(&activeVersion); err == sql.ErrNoRows {
		return nil, usage.ResultCandidateInvalid, nil
	} else if err != nil {
		return nil, "", err
	}
	if activeVersion.String != parentVersionID {
		result, updateErr := tx.ExecContext(ctx, `UPDATE build_candidates SET status='rejected' WHERE id=? AND status='pending'`, candidateID)
		if updateErr != nil {
			return nil, "", updateErr
		}
		changed, updateErr := result.RowsAffected()
		if updateErr != nil {
			return nil, "", updateErr
		}
		if changed != 1 {
			return nil, usage.ResultCandidateInvalid, nil
		}
		if err = appendAudit(ctx, tx, workspaceID, accountID, "candidate.rejected", "project", projectID, createdAt, map[string]any{"candidateId": candidateID, "reason": "stale_parent", "expectedParentVersionId": activeVersion.String, "actualParentVersionId": parentVersionID}); err != nil {
			return nil, "", err
		}
		if err = tx.Commit(); err != nil {
			return nil, "", err
		}
		return nil, usage.ResultVersionConflict, nil
	}
	if parentVersionID != "" {
		var parent int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM immutable_versions WHERE id=? AND workspace_id=? AND project_id=?`, parentVersionID, workspaceID, projectID).Scan(&parent); err != nil {
			return nil, "", err
		}
		if parent == 0 {
			return nil, usage.ResultCandidateInvalid, nil
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE build_candidates SET status='committed',committed_at=? WHERE id=? AND status='pending'`, createdAt, candidateID)
	if err != nil {
		return nil, "", err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, "", err
	}
	if changed != 1 {
		return nil, usage.ResultCandidateInvalid, nil
	}
	var versionID string
	if err = tx.QueryRowContext(ctx, `INSERT INTO immutable_versions (id,workspace_id,account_id,project_id,candidate_id,parent_version_id,prompt,snapshot_json,snapshot_hash,created_at) VALUES ('version_'||lower(hex(randomblob(16))),?,?,?,?,nullif(?,''),?,?,?,?) RETURNING id`, workspaceID, accountID, projectID, candidateID, parentVersionID, prompt, snapshotJSON, storedHash, createdAt).Scan(&versionID); err != nil {
		return nil, "", err
	}
	activation, err := tx.ExecContext(ctx, `UPDATE workspace_projects SET active_version_id=?,status='ready',updated_at=? WHERE id=? AND workspace_id=?`, versionID, createdAt, projectID, workspaceID)
	if err != nil {
		return nil, "", err
	}
	activated, err := activation.RowsAffected()
	if err != nil {
		return nil, "", err
	}
	if activated != 1 {
		return nil, usage.ResultCandidateInvalid, nil
	}
	if err = appendAudit(ctx, tx, workspaceID, accountID, "version.committed", "project", projectID, createdAt, map[string]any{"versionId": versionID, "candidateId": candidateID, "snapshotHash": storedHash, "sourceAction": sourceAction, "approvalId": approvalID}); err != nil {
		return nil, "", err
	}
	if err = tx.Commit(); err != nil {
		return nil, "", err
	}
	var snapshot domain.ProjectSnapshot
	if err = json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		return nil, "", err
	}
	return &usage.Version{ID: versionID, ProjectID: projectID, ParentVersionID: parentVersionID, Prompt: prompt, Snapshot: snapshot, CandidateID: candidateID, SnapshotHash: storedHash, SourceAction: sourceAction, ApprovalID: approvalID, CreatedAt: createdAt, Safety: &domain.SafetyVerification{Policy: safetyPolicy, VerifiedAt: safetyVerifiedAt}}, usage.ResultOK, nil
}

func (r *UsageRepository) StorePlan(ctx context.Context, workspaceID, accountID, projectID, prompt string, plan domain.BuildPlan, createdAt string) (string, error) {
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	var id string
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO approved_plans (id,workspace_id,account_id,project_id,prompt,plan_json,status,created_at)
		SELECT 'plan_'||lower(hex(randomblob(16))),?,?,?,?,?, 'pending', ?
		WHERE EXISTS (SELECT 1 FROM memberships WHERE workspace_id=? AND account_id=?)
		RETURNING id`, workspaceID, accountID, projectID, prompt, string(payload), createdAt, workspaceID, accountID).Scan(&id)
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

func (r *UsageRepository) ReserveApproved(ctx context.Context, accountID, workspaceID, projectID, approvalID, prompt string, credits int, createdAt string) (string, *domain.BuildPlan, usage.Result, error) {
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
	err = tx.QueryRowContext(ctx, `SELECT plan_json FROM approved_plans WHERE id=? AND workspace_id=? AND account_id=? AND project_id=? AND prompt=? AND status='approved'`, approvalID, workspaceID, accountID, projectID, prompt).Scan(&payload)
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
	if err = tx.QueryRowContext(ctx, `INSERT INTO agent_usage (id,workspace_id,account_id,project_id,approval_id,action,credits,status,created_at) VALUES (lower(hex(randomblob(16))),?,?,?,?,'build',?,'running',?) RETURNING id`, workspaceID, accountID, projectID, approvalID, credits, createdAt).Scan(&id); err != nil {
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
