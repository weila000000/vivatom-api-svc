package sqlite

import (
	"context"
	"database/sql"

	"vivatom-api-svc/internal/team"
)

type TeamRepository struct{ db *sql.DB }

func NewTeamRepository(db *sql.DB) *TeamRepository { return &TeamRepository{db: db} }

func (r *TeamRepository) ListMembers(ctx context.Context, accountID, workspaceID string) ([]team.Member, bool, error) {
	var allowed int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, workspaceID).Scan(&allowed); err != nil {
		return nil, false, err
	}
	if allowed == 0 {
		return nil, false, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT a.id,a.email,a.name,m.role,m.created_at FROM memberships m JOIN tenant_accounts a ON a.id=m.account_id WHERE m.workspace_id=? ORDER BY CASE m.role WHEN 'owner' THEN 0 ELSE 1 END,a.name`, workspaceID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	members := make([]team.Member, 0)
	for rows.Next() {
		var member team.Member
		if err := rows.Scan(&member.AccountID, &member.Email, &member.Name, &member.Role, &member.JoinedAt); err != nil {
			return nil, false, err
		}
		members = append(members, member)
	}
	return members, true, rows.Err()
}

func (r *TeamRepository) CreateInvitation(ctx context.Context, actorID string, invitation team.StoredInvitation) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var owner int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE workspace_id=? AND account_id=? AND role='owner'`, invitation.WorkspaceID, actorID).Scan(&owner); err != nil {
		return false, err
	}
	if owner == 0 {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM workspace_invitations WHERE workspace_id=? AND email=? AND accepted_at IS NULL`, invitation.WorkspaceID, invitation.Email); err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workspace_invitations (id,workspace_id,email,role,token_hash,invited_by,expires_at,created_at) VALUES (?,?,?,?,?,?,?,?)`, invitation.ID, invitation.WorkspaceID, invitation.Email, invitation.Role, invitation.TokenHash, invitation.InvitedBy, invitation.ExpiresAt, invitation.CreatedAt)
	if err != nil {
		return false, err
	}
	if err = appendAudit(ctx, tx, invitation.WorkspaceID, actorID, "member.invited", "invitation", invitation.ID, invitation.CreatedAt, map[string]any{"email": invitation.Email, "role": invitation.Role}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (r *TeamRepository) AcceptInvitation(ctx context.Context, accountID, email, tokenHash, now string) (*team.Member, team.Result, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	var workspaceID, invitedEmail, role string
	err = tx.QueryRowContext(ctx, `SELECT workspace_id,email,role FROM workspace_invitations WHERE token_hash=? AND accepted_at IS NULL AND expires_at>?`, tokenHash, now).Scan(&workspaceID, &invitedEmail, &role)
	if err == sql.ErrNoRows {
		return nil, team.ResultNotFound, nil
	}
	if err != nil {
		return nil, "", err
	}
	if invitedEmail != email {
		return nil, team.ResultEmailMismatch, nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memberships (workspace_id,account_id,role,created_at) VALUES (?,?,?,?) ON CONFLICT(workspace_id,account_id) DO NOTHING`, workspaceID, accountID, role, now); err != nil {
		return nil, "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE workspace_invitations SET accepted_at=? WHERE token_hash=?`, now, tokenHash); err != nil {
		return nil, "", err
	}
	var member team.Member
	if err = tx.QueryRowContext(ctx, `SELECT a.id,a.email,a.name,m.role,m.created_at FROM memberships m JOIN tenant_accounts a ON a.id=m.account_id WHERE m.workspace_id=? AND m.account_id=?`, workspaceID, accountID).Scan(&member.AccountID, &member.Email, &member.Name, &member.Role, &member.JoinedAt); err != nil {
		return nil, "", err
	}
	if err = appendAudit(ctx, tx, workspaceID, accountID, "member.invitation_accepted", "member", accountID, now, map[string]any{"email": email, "role": role}); err != nil {
		return nil, "", err
	}
	if err = tx.Commit(); err != nil {
		return nil, "", err
	}
	return &member, team.ResultOK, nil
}

func (r *TeamRepository) ChangeRole(ctx context.Context, actorID, workspaceID, targetID, role, now string) (team.Result, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	allowed, err := ownerInTx(ctx, tx, workspaceID, actorID)
	if err != nil {
		return "", err
	}
	if !allowed {
		return team.ResultForbidden, nil
	}
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE workspace_id=? AND account_id=?`, workspaceID, targetID).Scan(&current); err == sql.ErrNoRows {
		return team.ResultNotFound, nil
	} else if err != nil {
		return "", err
	}
	if current == "owner" && role == "member" {
		count, err := ownerCount(ctx, tx, workspaceID)
		if err != nil {
			return "", err
		}
		if count <= 1 {
			return team.ResultLastOwner, nil
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE memberships SET role=? WHERE workspace_id=? AND account_id=?`, role, workspaceID, targetID); err != nil {
		return "", err
	}
	if current != role {
		if err = appendAudit(ctx, tx, workspaceID, actorID, "member.role_changed", "member", targetID, now, map[string]any{"from": current, "to": role}); err != nil {
			return "", err
		}
	}
	return team.ResultOK, tx.Commit()
}

func (r *TeamRepository) RemoveMember(ctx context.Context, actorID, workspaceID, targetID, now string) (team.Result, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	allowed, err := ownerInTx(ctx, tx, workspaceID, actorID)
	if err != nil {
		return "", err
	}
	if !allowed {
		return team.ResultForbidden, nil
	}
	var role string
	if err = tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE workspace_id=? AND account_id=?`, workspaceID, targetID).Scan(&role); err == sql.ErrNoRows {
		return team.ResultNotFound, nil
	} else if err != nil {
		return "", err
	}
	if role == "owner" {
		count, err := ownerCount(ctx, tx, workspaceID)
		if err != nil {
			return "", err
		}
		if count <= 1 {
			return team.ResultLastOwner, nil
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM memberships WHERE workspace_id=? AND account_id=?`, workspaceID, targetID); err != nil {
		return "", err
	}
	if err = appendAudit(ctx, tx, workspaceID, actorID, "member.removed", "member", targetID, now, map[string]any{"role": role}); err != nil {
		return "", err
	}
	return team.ResultOK, tx.Commit()
}

func ownerInTx(ctx context.Context, tx *sql.Tx, workspaceID, accountID string) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE workspace_id=? AND account_id=? AND role='owner'`, workspaceID, accountID).Scan(&count)
	return count > 0, err
}
func ownerCount(ctx context.Context, tx *sql.Tx, workspaceID string) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE workspace_id=? AND role='owner'`, workspaceID).Scan(&count)
	return count, err
}
