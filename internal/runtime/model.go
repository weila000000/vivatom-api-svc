package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"vivatom-api-svc/internal/domain"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type StoredProject struct {
	ID             string
	Title          string
	Backend        domain.BackendSpec
	SchemaVersion  int
	PublicKeyHash  string
	AdminTokenHash string
	CreatedAt      string
	UpdatedAt      string
}

type ProvisionInput struct {
	ProjectID  string
	Title      string
	Backend    domain.BackendSpec
	PublicKey  string
	AdminToken string
}

type Inspection struct {
	ProjectID     string             `json:"projectId"`
	Title         string             `json:"title"`
	SchemaVersion int                `json:"schemaVersion"`
	Backend       domain.BackendSpec `json:"backend"`
}

type StoredUser struct {
	ID           string
	ProjectID    string
	Email        string
	PasswordSalt string
	PasswordHash string
	CreatedAt    string
}

type StoredSession struct {
	TokenHash string
	ProjectID string
	UserID    string
	ExpiresAt string
	CreatedAt string
}

type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	CreatedAt string `json:"createdAt"`
}

type Authentication struct {
	User      User   `json:"user"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

type StoredRecord struct {
	ID         string
	ProjectID  string
	Collection string
	OwnerID    *string
	Data       map[string]any
	CreatedAt  string
	UpdatedAt  string
}

type Record struct {
	ID        string         `json:"id"`
	Data      map[string]any `json:"-"`
	CreatedAt string         `json:"createdAt"`
	UpdatedAt string         `json:"updatedAt"`
}

func (r Record) MarshalJSON() ([]byte, error) {
	result := make(map[string]any, len(r.Data)+3)
	for key, value := range r.Data {
		result[key] = value
	}
	result["id"] = r.ID
	result["createdAt"] = r.CreatedAt
	result["updatedAt"] = r.UpdatedAt
	return json.Marshal(result)
}

type RecordScope struct {
	ProjectID  string
	Collection string
	OwnerID    *string
}

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string { return e.Code }

func validateProvision(input ProvisionInput) error {
	if !uuidPattern.MatchString(strings.ToLower(input.ProjectID)) ||
		strings.TrimSpace(input.Title) == "" ||
		len([]rune(input.Title)) > 80 ||
		len(input.PublicKey) < 16 ||
		len(input.PublicKey) > 256 ||
		len(input.AdminToken) < 16 ||
		len(input.AdminToken) > 256 ||
		!input.Backend.Enabled {
		return errors.New("invalid provision input")
	}
	if input.Backend.Auth != "none" && input.Backend.Auth != "email_password" {
		return errors.New("invalid auth mode")
	}
	collections := make(map[string]bool, len(input.Backend.Collections))
	for _, collection := range input.Backend.Collections {
		if !identifierPattern.MatchString(collection.Name) ||
			strings.TrimSpace(collection.Label) == "" ||
			(collection.Access != "public" && collection.Access != "owner") ||
			collections[collection.Name] {
			return errors.New("invalid collection")
		}
		if collection.Access == "owner" && input.Backend.Auth != "email_password" {
			return errors.New("owner collection requires authentication")
		}
		collections[collection.Name] = true
		fields := make(map[string]bool, len(collection.Fields))
		for _, field := range collection.Fields {
			if !identifierPattern.MatchString(field.Name) ||
				strings.TrimSpace(field.Label) == "" ||
				!validFieldType(field.Type) ||
				fields[field.Name] {
				return errors.New("invalid field")
			}
			fields[field.Name] = true
		}
	}
	return nil
}

func validFieldType(value string) bool {
	switch value {
	case "text", "long_text", "number", "boolean", "date":
		return true
	default:
		return false
	}
}

func ensureCompatible(previous, next domain.BackendSpec) error {
	if previous.Enabled && !next.Enabled {
		return errors.New("backend cannot be disabled")
	}
	if previous.Auth == "email_password" && next.Auth != "email_password" {
		return errors.New("authentication cannot be removed")
	}
	for _, oldCollection := range previous.Collections {
		newCollection, ok := findCollection(next, oldCollection.Name)
		if !ok {
			return fmt.Errorf("collection removed: %s", oldCollection.Name)
		}
		if oldCollection.Access != newCollection.Access {
			return fmt.Errorf("collection access changed: %s", oldCollection.Name)
		}
		for _, oldField := range oldCollection.Fields {
			newField, ok := findField(newCollection, oldField.Name)
			if !ok || oldField.Type != newField.Type || oldField.Required != newField.Required {
				return fmt.Errorf("field changed: %s.%s", oldCollection.Name, oldField.Name)
			}
		}
		for _, newField := range newCollection.Fields {
			if _, ok := findField(oldCollection, newField.Name); !ok && newField.Required {
				return fmt.Errorf("new field must be optional: %s.%s", newCollection.Name, newField.Name)
			}
		}
	}
	return nil
}

func findCollection(spec domain.BackendSpec, name string) (domain.BackendCollection, bool) {
	for _, collection := range spec.Collections {
		if collection.Name == name {
			return collection, true
		}
	}
	return domain.BackendCollection{}, false
}

func findField(collection domain.BackendCollection, name string) (domain.BackendField, bool) {
	for _, field := range collection.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return domain.BackendField{}, false
}
