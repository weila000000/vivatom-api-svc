package runtime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
	"vivatom-api-svc/internal/domain"
)

var ErrConflict = errors.New("runtime project conflict")

type Repository interface {
	GetProject(context.Context, string) (*StoredProject, error)
	CreateProject(context.Context, StoredProject) error
	UpdateBackend(context.Context, string, int, domain.BackendSpec, string) (bool, error)
	GetUserByEmail(context.Context, string, string) (*StoredUser, error)
	CreateUser(context.Context, StoredUser) error
	CreateSession(context.Context, StoredSession) error
	GetSessionUser(context.Context, string, string, string) (*StoredUser, error)
	DeleteSession(context.Context, string, string) error
	InsertRecordWithinLimit(context.Context, StoredRecord, int) (bool, error)
	ListRecords(context.Context, RecordScope, int) ([]StoredRecord, error)
	GetRecord(context.Context, RecordScope, string) (*StoredRecord, error)
	UpdateRecord(context.Context, RecordScope, string, map[string]any, string) (bool, error)
	DeleteRecord(context.Context, RecordScope, string) (bool, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) Provision(ctx context.Context, input ProvisionInput) (int, error) {
	if err := validateProvision(input); err != nil {
		return 0, &Error{Code: "invalid_request", Status: 400}
	}
	publicHash := tokenHash(input.PublicKey)
	adminHash := tokenHash(input.AdminToken)

	for attempt := 0; attempt < 2; attempt++ {
		existing, err := s.repository.GetProject(ctx, input.ProjectID)
		if err != nil {
			return 0, internalError()
		}
		if existing == nil {
			now := s.now().UTC().Format(time.RFC3339Nano)
			err = s.repository.CreateProject(ctx, StoredProject{
				ID: input.ProjectID, Title: input.Title, Backend: input.Backend,
				SchemaVersion: 1, PublicKeyHash: publicHash, AdminTokenHash: adminHash,
				CreatedAt: now, UpdatedAt: now,
			})
			if err == nil {
				return 1, nil
			}
			if errors.Is(err, ErrConflict) {
				continue
			}
			return 0, internalError()
		}
		if !sameHash(existing.PublicKeyHash, publicHash) ||
			!sameHash(existing.AdminTokenHash, adminHash) {
			return 0, &Error{Code: "forbidden", Status: 403}
		}
		if sameBackend(existing.Backend, input.Backend) {
			return existing.SchemaVersion, nil
		}
		if err := ensureCompatible(existing.Backend, input.Backend); err != nil {
			return 0, &Error{Code: "schema_conflict", Status: 409}
		}
		nextVersion := existing.SchemaVersion + 1
		updated, err := s.repository.UpdateBackend(
			ctx, input.ProjectID, existing.SchemaVersion, input.Backend,
			s.now().UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return 0, internalError()
		}
		if updated {
			return nextVersion, nil
		}
	}
	return 0, &Error{Code: "provision_conflict", Status: 409}
}

func (s *Service) Inspect(
	ctx context.Context,
	projectID string,
	publicKey string,
	adminToken string,
) (Inspection, error) {
	project, err := s.repository.GetProject(ctx, projectID)
	if err != nil {
		return Inspection{}, internalError()
	}
	if project == nil ||
		!sameHash(project.PublicKeyHash, tokenHash(publicKey)) ||
		!sameHash(project.AdminTokenHash, tokenHash(adminToken)) {
		return Inspection{}, &Error{Code: "forbidden", Status: 403}
	}
	return Inspection{
		ProjectID: project.ID, Title: project.Title,
		SchemaVersion: project.SchemaVersion, Backend: project.Backend,
	}, nil
}

func (s *Service) Register(ctx context.Context, projectID, publicKey, email, password string) (Authentication, error) {
	project, err := s.publicProject(ctx, projectID, publicKey)
	if err != nil {
		return Authentication{}, err
	}
	if project.Backend.Auth != "email_password" {
		return Authentication{}, &Error{Code: "auth_disabled", Status: 409}
	}
	email, ok := normalizeCredentials(email, password)
	if !ok {
		return Authentication{}, &Error{Code: "invalid_request", Status: 400}
	}
	existing, err := s.repository.GetUserByEmail(ctx, projectID, email)
	if err != nil {
		return Authentication{}, internalError()
	}
	if existing != nil {
		return Authentication{}, &Error{Code: "email_taken", Status: 409}
	}
	salt, err := randomToken(16)
	if err != nil {
		return Authentication{}, internalError()
	}
	digest, err := passwordDigest(password, salt)
	if err != nil {
		return Authentication{}, internalError()
	}
	now := s.now().UTC()
	user := StoredUser{ID: newID(now), ProjectID: projectID, Email: email, PasswordSalt: salt, PasswordHash: digest, CreatedAt: now.Format(time.RFC3339Nano)}
	if err := s.repository.CreateUser(ctx, user); err != nil {
		if errors.Is(err, ErrConflict) {
			return Authentication{}, &Error{Code: "email_taken", Status: 409}
		}
		return Authentication{}, internalError()
	}
	return s.createAuthentication(ctx, user, now)
}

func (s *Service) Login(ctx context.Context, projectID, publicKey, email, password string) (Authentication, error) {
	project, err := s.publicProject(ctx, projectID, publicKey)
	if err != nil {
		return Authentication{}, err
	}
	if project.Backend.Auth != "email_password" {
		return Authentication{}, &Error{Code: "auth_disabled", Status: 409}
	}
	email, ok := normalizeCredentials(email, password)
	if !ok {
		return Authentication{}, &Error{Code: "invalid_credentials", Status: 401}
	}
	user, err := s.repository.GetUserByEmail(ctx, projectID, email)
	if err != nil {
		return Authentication{}, internalError()
	}
	if user == nil {
		return Authentication{}, &Error{Code: "invalid_credentials", Status: 401}
	}
	digest, err := passwordDigest(password, user.PasswordSalt)
	if err != nil || !sameHash(user.PasswordHash, digest) {
		return Authentication{}, &Error{Code: "invalid_credentials", Status: 401}
	}
	return s.createAuthentication(ctx, *user, s.now().UTC())
}

func (s *Service) Me(ctx context.Context, projectID, publicKey, token string) (User, error) {
	if _, err := s.publicProject(ctx, projectID, publicKey); err != nil {
		return User{}, err
	}
	user, err := s.repository.GetSessionUser(ctx, projectID, tokenHash(token), s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return User{}, internalError()
	}
	if user == nil {
		return User{}, &Error{Code: "unauthorized", Status: 401}
	}
	return publicUser(*user), nil
}

func (s *Service) Logout(ctx context.Context, projectID, publicKey, token string) error {
	if _, err := s.publicProject(ctx, projectID, publicKey); err != nil {
		return err
	}
	if token == "" {
		return &Error{Code: "unauthorized", Status: 401}
	}
	if err := s.repository.DeleteSession(ctx, projectID, tokenHash(token)); err != nil {
		return internalError()
	}
	return nil
}

func (s *Service) CreateRecord(ctx context.Context, projectID, publicKey, token, collectionName string, input map[string]any) (Record, error) {
	project, collection, ownerID, err := s.recordContext(ctx, projectID, publicKey, token, collectionName)
	if err != nil {
		return Record{}, err
	}
	data, err := validateRecord(collection, input, true)
	if err != nil {
		return Record{}, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	id, err := randomID("rec")
	if err != nil {
		return Record{}, internalError()
	}
	stored := StoredRecord{ID: id, ProjectID: project.ID, Collection: collection.Name, OwnerID: ownerID, Data: data, CreatedAt: now, UpdatedAt: now}
	created, err := s.repository.InsertRecordWithinLimit(ctx, stored, 1000)
	if err != nil {
		return Record{}, internalError()
	}
	if !created {
		return Record{}, &Error{Code: "resource_limit", Status: 429}
	}
	return publicRecord(stored), nil
}

func (s *Service) ListRecords(ctx context.Context, projectID, publicKey, token, collectionName string) ([]Record, error) {
	project, collection, ownerID, err := s.recordContext(ctx, projectID, publicKey, token, collectionName)
	if err != nil {
		return nil, err
	}
	stored, err := s.repository.ListRecords(ctx, RecordScope{ProjectID: project.ID, Collection: collection.Name, OwnerID: ownerID}, 100)
	if err != nil {
		return nil, internalError()
	}
	records := make([]Record, 0, len(stored))
	for _, item := range stored {
		records = append(records, publicRecord(item))
	}
	return records, nil
}

func (s *Service) UpdateRecord(ctx context.Context, projectID, publicKey, token, collectionName, recordID string, patch map[string]any) (Record, error) {
	project, collection, ownerID, err := s.recordContext(ctx, projectID, publicKey, token, collectionName)
	if err != nil {
		return Record{}, err
	}
	changes, err := validateRecord(collection, patch, false)
	if err != nil {
		return Record{}, err
	}
	scope := RecordScope{ProjectID: project.ID, Collection: collection.Name, OwnerID: ownerID}
	current, err := s.repository.GetRecord(ctx, scope, recordID)
	if err != nil {
		return Record{}, internalError()
	}
	if current == nil {
		return Record{}, &Error{Code: "not_found", Status: 404}
	}
	merged := cloneData(current.Data)
	for key, value := range changes {
		merged[key] = value
	}
	merged, err = validateRecord(collection, merged, true)
	if err != nil {
		return Record{}, err
	}
	updatedAt := s.now().UTC().Format(time.RFC3339Nano)
	updated, err := s.repository.UpdateRecord(ctx, scope, recordID, merged, updatedAt)
	if err != nil {
		return Record{}, internalError()
	}
	if !updated {
		return Record{}, &Error{Code: "not_found", Status: 404}
	}
	current.Data, current.UpdatedAt = merged, updatedAt
	return publicRecord(*current), nil
}

func (s *Service) DeleteRecord(ctx context.Context, projectID, publicKey, token, collectionName, recordID string) error {
	project, collection, ownerID, err := s.recordContext(ctx, projectID, publicKey, token, collectionName)
	if err != nil {
		return err
	}
	deleted, err := s.repository.DeleteRecord(ctx, RecordScope{ProjectID: project.ID, Collection: collection.Name, OwnerID: ownerID}, recordID)
	if err != nil {
		return internalError()
	}
	if !deleted {
		return &Error{Code: "not_found", Status: 404}
	}
	return nil
}

func (s *Service) recordContext(ctx context.Context, projectID, publicKey, token, collectionName string) (*StoredProject, domain.BackendCollection, *string, error) {
	project, err := s.publicProject(ctx, projectID, publicKey)
	if err != nil {
		return nil, domain.BackendCollection{}, nil, err
	}
	collection, ok := findCollection(project.Backend, collectionName)
	if !ok {
		return nil, domain.BackendCollection{}, nil, &Error{Code: "not_found", Status: 404}
	}
	if collection.Access == "public" {
		return project, collection, nil, nil
	}
	if token == "" {
		return nil, domain.BackendCollection{}, nil, &Error{Code: "unauthorized", Status: 401}
	}
	user, err := s.repository.GetSessionUser(ctx, projectID, tokenHash(token), s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, domain.BackendCollection{}, nil, internalError()
	}
	if user == nil {
		return nil, domain.BackendCollection{}, nil, &Error{Code: "unauthorized", Status: 401}
	}
	return project, collection, &user.ID, nil
}

func (s *Service) publicProject(ctx context.Context, projectID, publicKey string) (*StoredProject, error) {
	project, err := s.repository.GetProject(ctx, projectID)
	if err != nil {
		return nil, internalError()
	}
	if project == nil || publicKey == "" || !sameHash(project.PublicKeyHash, tokenHash(publicKey)) {
		return nil, &Error{Code: "forbidden", Status: 403}
	}
	return project, nil
}

func (s *Service) createAuthentication(ctx context.Context, user StoredUser, now time.Time) (Authentication, error) {
	token, err := randomToken(32)
	if err != nil {
		return Authentication{}, internalError()
	}
	expires := now.Add(7 * 24 * time.Hour)
	if err := s.repository.CreateSession(ctx, StoredSession{TokenHash: tokenHash(token), ProjectID: user.ProjectID, UserID: user.ID, ExpiresAt: expires.Format(time.RFC3339Nano), CreatedAt: now.Format(time.RFC3339Nano)}); err != nil {
		return Authentication{}, internalError()
	}
	return Authentication{User: publicUser(user), Token: token, ExpiresAt: expires.Format(time.RFC3339Nano)}, nil
}

func normalizeCredentials(email, password string) (string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	return email, err == nil && address.Address == email && len(email) <= 254 && len(password) >= 8 && len(password) <= 128
}

func validateRecord(collection domain.BackendCollection, input map[string]any, create bool) (map[string]any, error) {
	if input == nil || (!create && len(input) == 0) {
		return nil, &Error{Code: "invalid_record", Status: 400}
	}
	fields := make(map[string]domain.BackendField, len(collection.Fields))
	for _, field := range collection.Fields {
		fields[field.Name] = field
	}
	reserved := map[string]bool{"id": true, "owner_id": true, "created_at": true, "updated_at": true, "ownerId": true, "createdAt": true, "updatedAt": true}
	for name, value := range input {
		field, ok := fields[name]
		if !ok || reserved[name] || !validRecordValue(field, value) {
			return nil, &Error{Code: "invalid_record", Status: 400}
		}
	}
	if create {
		for _, field := range collection.Fields {
			if _, ok := input[field.Name]; field.Required && !ok {
				return nil, &Error{Code: "invalid_record", Status: 400}
			}
		}
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > 32*1024 {
		return nil, &Error{Code: "invalid_record", Status: 400}
	}
	return cloneData(input), nil
}

func validRecordValue(field domain.BackendField, value any) bool {
	switch field.Type {
	case "text", "long_text":
		text, ok := value.(string)
		limit := 500
		if field.Type == "long_text" {
			limit = 5000
		}
		return ok && len([]rune(text)) <= limit && (!field.Required || strings.TrimSpace(text) != "")
	case "number":
		number, ok := value.(float64)
		return ok && !math.IsNaN(number) && !math.IsInf(number, 0)
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "date":
		text, ok := value.(string)
		if !ok || len(text) > 64 {
			return false
		}
		_, err := time.Parse(time.RFC3339, text)
		return err == nil
	default:
		return false
	}
}

func cloneData(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func publicRecord(record StoredRecord) Record {
	return Record{ID: record.ID, Data: cloneData(record.Data), CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
}

func passwordDigest(password, salt string) (string, error) {
	saltBytes, err := base64.RawURLEncoding.DecodeString(salt)
	if err != nil {
		return "", err
	}
	digest, err := scrypt.Key([]byte(password), saltBytes, 32768, 8, 1, 32)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(digest), nil
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func newID(now time.Time) string {
	random, _ := randomID("usr")
	return fmt.Sprintf("%s_%d", random, now.UnixMilli())
}

func randomID(prefix string) (string, error) {
	random, err := randomToken(12)
	if err != nil {
		return "", err
	}
	return prefix + "_" + random, nil
}

func publicUser(user StoredUser) User {
	return User{ID: user.ID, Email: user.Email, CreatedAt: user.CreatedAt}
}

func tokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sameHash(expected, actual string) bool {
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func sameBackend(left, right domain.BackendSpec) bool {
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && string(a) == string(b)
}

func internalError() error {
	return &Error{Code: "runtime_unavailable", Status: 503}
}
