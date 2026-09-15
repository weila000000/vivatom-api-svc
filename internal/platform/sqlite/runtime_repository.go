package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"vivatom-api-svc/internal/domain"
	runtimeservice "vivatom-api-svc/internal/runtime"
)

type RuntimeRepository struct {
	db *sql.DB
}

func NewRuntimeRepository(db *sql.DB) *RuntimeRepository {
	return &RuntimeRepository{db: db}
}

func (r *RuntimeRepository) GetProject(ctx context.Context, id string) (*runtimeservice.StoredProject, error) {
	var project runtimeservice.StoredProject
	var backendJSON string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, title, backend_json, schema_version, public_key_hash,
		       admin_token_hash, created_at, updated_at
		FROM runtime_projects WHERE id = ?
	`, id).Scan(
		&project.ID, &project.Title, &backendJSON, &project.SchemaVersion,
		&project.PublicKeyHash, &project.AdminTokenHash,
		&project.CreatedAt, &project.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(backendJSON), &project.Backend); err != nil {
		return nil, err
	}
	return &project, nil
}

func (r *RuntimeRepository) CreateProject(ctx context.Context, project runtimeservice.StoredProject) error {
	backendJSON, err := json.Marshal(project.Backend)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO runtime_projects
		  (id, title, backend_json, schema_version, public_key_hash,
		   admin_token_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, project.ID, project.Title, backendJSON, project.SchemaVersion,
		project.PublicKeyHash, project.AdminTokenHash,
		project.CreatedAt, project.UpdatedAt,
	)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "constraint") {
		return runtimeservice.ErrConflict
	}
	return err
}

func (r *RuntimeRepository) UpdateBackend(
	ctx context.Context,
	id string,
	currentVersion int,
	backend domain.BackendSpec,
	updatedAt string,
) (bool, error) {
	backendJSON, err := json.Marshal(backend)
	if err != nil {
		return false, err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE runtime_projects
		SET backend_json = ?, schema_version = schema_version + 1, updated_at = ?
		WHERE id = ? AND schema_version = ?
	`, backendJSON, updatedAt, id, currentVersion)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *RuntimeRepository) GetUserByEmail(ctx context.Context, projectID, email string) (*runtimeservice.StoredUser, error) {
	var user runtimeservice.StoredUser
	err := r.db.QueryRowContext(ctx, `
		SELECT id, project_id, email, password_salt, password_hash, created_at
		FROM runtime_users WHERE project_id = ? AND email = ?
	`, projectID, email).Scan(&user.ID, &user.ProjectID, &user.Email, &user.PasswordSalt, &user.PasswordHash, &user.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &user, err
}

func (r *RuntimeRepository) CreateUser(ctx context.Context, user runtimeservice.StoredUser) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO runtime_users (id, project_id, email, password_salt, password_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, user.ID, user.ProjectID, user.Email, user.PasswordSalt, user.PasswordHash, user.CreatedAt)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "constraint") {
		return runtimeservice.ErrConflict
	}
	return err
}

func (r *RuntimeRepository) CreateSession(ctx context.Context, session runtimeservice.StoredSession) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO runtime_sessions (token_hash, project_id, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, session.TokenHash, session.ProjectID, session.UserID, session.ExpiresAt, session.CreatedAt)
	return err
}

func (r *RuntimeRepository) GetSessionUser(ctx context.Context, projectID, tokenHash, now string) (*runtimeservice.StoredUser, error) {
	var user runtimeservice.StoredUser
	err := r.db.QueryRowContext(ctx, `
		SELECT u.id, u.project_id, u.email, u.password_salt, u.password_hash, u.created_at
		FROM runtime_sessions s
		JOIN runtime_users u ON u.id = s.user_id AND u.project_id = s.project_id
		WHERE s.project_id = ? AND s.token_hash = ? AND s.expires_at > ?
	`, projectID, tokenHash, now).Scan(&user.ID, &user.ProjectID, &user.Email, &user.PasswordSalt, &user.PasswordHash, &user.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &user, err
}

func (r *RuntimeRepository) DeleteSession(ctx context.Context, projectID, tokenHash string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM runtime_sessions WHERE project_id = ? AND token_hash = ?
	`, projectID, tokenHash)
	return err
}

func (r *RuntimeRepository) InsertRecordWithinLimit(ctx context.Context, record runtimeservice.StoredRecord, limit int) (bool, error) {
	dataJSON, err := json.Marshal(record.Data)
	if err != nil {
		return false, err
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO runtime_records
		  (id, project_id, collection_name, owner_id, data_json, created_at, updated_at)
		SELECT ?, ?, ?, ?, ?, ?, ?
		WHERE (SELECT COUNT(*) FROM runtime_records
		       WHERE project_id = ? AND collection_name = ?) < ?
	`, record.ID, record.ProjectID, record.Collection, record.OwnerID, dataJSON,
		record.CreatedAt, record.UpdatedAt, record.ProjectID, record.Collection, limit)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *RuntimeRepository) ListRecords(ctx context.Context, scope runtimeservice.RecordScope, limit int) ([]runtimeservice.StoredRecord, error) {
	query := `SELECT id, project_id, collection_name, owner_id, data_json, created_at, updated_at
		FROM runtime_records WHERE project_id = ? AND collection_name = ?`
	args := []any{scope.ProjectID, scope.Collection}
	query, args = addOwnerScope(query, args, scope.OwnerID)
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]runtimeservice.StoredRecord, 0)
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}
	return records, rows.Err()
}

func (r *RuntimeRepository) GetRecord(ctx context.Context, scope runtimeservice.RecordScope, id string) (*runtimeservice.StoredRecord, error) {
	query := `SELECT id, project_id, collection_name, owner_id, data_json, created_at, updated_at
		FROM runtime_records WHERE id = ? AND project_id = ? AND collection_name = ?`
	args := []any{id, scope.ProjectID, scope.Collection}
	query, args = addOwnerScope(query, args, scope.OwnerID)
	record, err := scanRecord(r.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (r *RuntimeRepository) UpdateRecord(ctx context.Context, scope runtimeservice.RecordScope, id string, data map[string]any, updatedAt string) (bool, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return false, err
	}
	query := `UPDATE runtime_records SET data_json = ?, updated_at = ?
		WHERE id = ? AND project_id = ? AND collection_name = ?`
	args := []any{dataJSON, updatedAt, id, scope.ProjectID, scope.Collection}
	query, args = addOwnerScope(query, args, scope.OwnerID)
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *RuntimeRepository) DeleteRecord(ctx context.Context, scope runtimeservice.RecordScope, id string) (bool, error) {
	query := `DELETE FROM runtime_records WHERE id = ? AND project_id = ? AND collection_name = ?`
	args := []any{id, scope.ProjectID, scope.Collection}
	query, args = addOwnerScope(query, args, scope.OwnerID)
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

type rowScanner interface {
	Scan(...any) error
}

func scanRecord(row rowScanner) (*runtimeservice.StoredRecord, error) {
	var record runtimeservice.StoredRecord
	var dataJSON string
	if err := row.Scan(&record.ID, &record.ProjectID, &record.Collection, &record.OwnerID, &dataJSON, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(dataJSON), &record.Data); err != nil {
		return nil, err
	}
	return &record, nil
}

func addOwnerScope(query string, args []any, ownerID *string) (string, []any) {
	if ownerID == nil {
		return query, args
	}
	return query + ` AND owner_id = ?`, append(args, *ownerID)
}
