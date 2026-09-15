package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"vivatom-api-svc/internal/identity"
)

type IdentityRepository struct{ db *sql.DB }

func NewIdentityRepository(db *sql.DB) *IdentityRepository { return &IdentityRepository{db: db} }

func (r *IdentityRepository) CreateAccountWorkspace(ctx context.Context, account identity.StoredAccount, workspace identity.Workspace) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO tenant_accounts (id,email,name,password_salt,password_hash,created_at) VALUES (?,?,?,?,?,?)`, account.ID, account.Email, account.Name, account.PasswordSalt, account.PasswordHash, account.CreatedAt); err != nil {
		return identityConstraint(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspaces (id,name,created_at) VALUES (?,?,?)`, workspace.ID, workspace.Name, workspace.CreatedAt); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO memberships (workspace_id,account_id,role,created_at) VALUES (?,?,?,?)`, workspace.ID, account.ID, "owner", workspace.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *IdentityRepository) GetAccountByEmail(ctx context.Context, email string) (*identity.StoredAccount, error) {
	return scanAccount(r.db.QueryRowContext(ctx, `SELECT id,email,name,password_salt,password_hash,created_at FROM tenant_accounts WHERE email = ?`, email))
}

func (r *IdentityRepository) CreateSession(ctx context.Context, tokenHash, accountID, expiresAt, createdAt string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO platform_sessions (token_hash,account_id,expires_at,created_at) VALUES (?,?,?,?)`, tokenHash, accountID, expiresAt, createdAt)
	return err
}

func (r *IdentityRepository) GetSessionAccount(ctx context.Context, tokenHash, now string) (*identity.StoredAccount, error) {
	return scanAccount(r.db.QueryRowContext(ctx, `SELECT a.id,a.email,a.name,a.password_salt,a.password_hash,a.created_at FROM platform_sessions s JOIN tenant_accounts a ON a.id=s.account_id WHERE s.token_hash=? AND s.expires_at>?`, tokenHash, now))
}

func (r *IdentityRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM platform_sessions WHERE token_hash=?`, tokenHash)
	return err
}

func (r *IdentityRepository) ListWorkspaces(ctx context.Context, accountID string) ([]identity.Workspace, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT w.id,w.name,m.role,w.created_at FROM memberships m JOIN workspaces w ON w.id=m.workspace_id WHERE m.account_id=? ORDER BY w.created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]identity.Workspace, 0)
	for rows.Next() {
		var workspace identity.Workspace
		if err := rows.Scan(&workspace.ID, &workspace.Name, &workspace.Role, &workspace.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, workspace)
	}
	return result, rows.Err()
}

type accountScanner interface{ Scan(...any) error }

func scanAccount(row accountScanner) (*identity.StoredAccount, error) {
	var account identity.StoredAccount
	err := row.Scan(&account.ID, &account.Email, &account.Name, &account.PasswordSalt, &account.PasswordHash, &account.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &account, err
}

func identityConstraint(err error) error {
	if strings.Contains(strings.ToLower(err.Error()), "constraint") {
		return identity.ErrConflict
	}
	return err
}
