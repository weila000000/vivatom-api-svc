package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

var ErrConflict = errors.New("identity conflict")

type Repository interface {
	CreateAccountWorkspace(context.Context, StoredAccount, Workspace) error
	GetAccountByEmail(context.Context, string) (*StoredAccount, error)
	CreateSession(context.Context, string, string, string, string) error
	GetSessionAccount(context.Context, string, string) (*StoredAccount, error)
	DeleteSession(context.Context, string) error
	ListWorkspaces(context.Context, string) ([]Workspace, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) Register(ctx context.Context, input Registration) (Authentication, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	name, workspaceName := strings.TrimSpace(input.Name), strings.TrimSpace(input.WorkspaceName)
	if !validEmail(email) || len(input.Password) < 8 || len(input.Password) > 128 || name == "" || len([]rune(name)) > 60 || workspaceName == "" || len([]rune(workspaceName)) > 80 {
		return Authentication{}, &Error{Code: "invalid_request", Status: 400}
	}
	salt, err := randomValue(16)
	if err != nil {
		return Authentication{}, unavailable()
	}
	digest, err := passwordDigest(input.Password, salt)
	if err != nil {
		return Authentication{}, unavailable()
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	accountID, err := randomID("acct")
	if err != nil {
		return Authentication{}, unavailable()
	}
	workspaceID, err := randomID("ws")
	if err != nil {
		return Authentication{}, unavailable()
	}
	account := StoredAccount{Account: Account{ID: accountID, Email: email, Name: name, CreatedAt: now}, PasswordSalt: salt, PasswordHash: digest}
	workspace := Workspace{ID: workspaceID, Name: workspaceName, Role: "owner", CreatedAt: now}
	if err := s.repository.CreateAccountWorkspace(ctx, account, workspace); err != nil {
		if errors.Is(err, ErrConflict) {
			return Authentication{}, &Error{Code: "email_taken", Status: 409}
		}
		return Authentication{}, unavailable()
	}
	return s.issue(ctx, account, []Workspace{workspace})
}

func (s *Service) Login(ctx context.Context, email, password string) (Authentication, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !validEmail(email) || len(password) > 128 {
		return Authentication{}, invalidCredentials()
	}
	account, err := s.repository.GetAccountByEmail(ctx, email)
	if err != nil {
		return Authentication{}, unavailable()
	}
	if account == nil {
		return Authentication{}, invalidCredentials()
	}
	digest, err := passwordDigest(password, account.PasswordSalt)
	if err != nil || subtle.ConstantTimeCompare([]byte(account.PasswordHash), []byte(digest)) != 1 {
		return Authentication{}, invalidCredentials()
	}
	workspaces, err := s.repository.ListWorkspaces(ctx, account.ID)
	if err != nil {
		return Authentication{}, unavailable()
	}
	return s.issue(ctx, *account, workspaces)
}

func (s *Service) Me(ctx context.Context, token string) (Account, []Workspace, error) {
	account, err := s.accountForToken(ctx, token)
	if err != nil {
		return Account{}, nil, err
	}
	workspaces, err := s.repository.ListWorkspaces(ctx, account.ID)
	if err != nil {
		return Account{}, nil, unavailable()
	}
	return account.Account, workspaces, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if _, err := s.accountForToken(ctx, token); err != nil {
		return err
	}
	if err := s.repository.DeleteSession(ctx, tokenHash(token)); err != nil {
		return unavailable()
	}
	return nil
}

func (s *Service) accountForToken(ctx context.Context, token string) (*StoredAccount, error) {
	if token == "" {
		return nil, unauthorized()
	}
	account, err := s.repository.GetSessionAccount(ctx, tokenHash(token), s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, unavailable()
	}
	if account == nil {
		return nil, unauthorized()
	}
	return account, nil
}

func (s *Service) issue(ctx context.Context, account StoredAccount, workspaces []Workspace) (Authentication, error) {
	token, err := randomValue(32)
	if err != nil {
		return Authentication{}, unavailable()
	}
	now := s.now().UTC()
	expires := now.Add(14 * 24 * time.Hour)
	if err := s.repository.CreateSession(ctx, tokenHash(token), account.ID, expires.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return Authentication{}, unavailable()
	}
	return Authentication{User: account.Account, Session: Session{Token: token, ExpiresAt: expires.Format(time.RFC3339Nano)}, Workspaces: workspaces}, nil
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && len(value) <= 254
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
func randomValue(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
func randomID(prefix string) (string, error) {
	value, err := randomValue(12)
	if err != nil {
		return "", err
	}
	return prefix + "_" + value, nil
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func invalidCredentials() error { return &Error{Code: "invalid_credentials", Status: 401} }
func unauthorized() error       { return &Error{Code: "unauthorized", Status: 401} }
func unavailable() error        { return &Error{Code: "identity_unavailable", Status: 503} }
