package domain

import (
	"path"
	"regexp"
	"strings"
)

var planIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

func ValidateBuildPlan(plan BuildPlan) error {
	if plan.ProductType != "website" && plan.ProductType != "web_app" {
		return &ContractError{Code: "plan.product_type_invalid"}
	}
	if !boundedText(plan.ProductSummary, 2000) {
		return &ContractError{Code: "plan.summary_invalid"}
	}
	if !boundedText(plan.DesignDirection, 2000) {
		return &ContractError{Code: "plan.design_invalid"}
	}
	if err := validateTextList(plan.TargetUsers, 1, 20, 300, "plan.target_users_invalid"); err != nil {
		return err
	}
	if err := validateTextList(plan.Features, 1, 50, 500, "plan.features_invalid"); err != nil {
		return err
	}
	if err := validateTextList(plan.AcceptanceChecks, 1, 50, 500, "plan.acceptance_invalid"); err != nil {
		return err
	}
	if len(plan.Pages) == 0 || len(plan.Pages) > 30 {
		return &ContractError{Code: "plan.pages_invalid"}
	}
	for _, page := range plan.Pages {
		if !boundedText(page.Name, 120) || !boundedText(page.Purpose, 500) {
			return &ContractError{Code: "plan.page_invalid", Path: page.Name}
		}
	}
	if len(plan.FilePlan) == 0 || len(plan.FilePlan) > 80 {
		return &ContractError{Code: "plan.files_invalid"}
	}
	seenPaths := make(map[string]bool, len(plan.FilePlan))
	for _, file := range plan.FilePlan {
		if !safePlanPath(file.Path) || !boundedText(file.Responsibility, 500) || seenPaths[file.Path] {
			return &ContractError{Code: "plan.file_invalid", Path: file.Path}
		}
		seenPaths[file.Path] = true
	}
	return validateBackendPlan(plan.Backend)
}

func validateBackendPlan(backend BackendSpec) error {
	if backend.Auth != "none" && backend.Auth != "email_password" {
		return &ContractError{Code: "plan.backend_auth_invalid"}
	}
	if !backend.Enabled && (backend.Auth != "none" || len(backend.Collections) > 0) {
		return &ContractError{Code: "plan.backend_disabled_invalid"}
	}
	if len(backend.Collections) > 30 {
		return &ContractError{Code: "plan.collections_invalid"}
	}
	collections := map[string]bool{}
	for _, collection := range backend.Collections {
		if !planIdentifier.MatchString(collection.Name) || !boundedText(collection.Label, 120) || collections[collection.Name] {
			return &ContractError{Code: "plan.collection_invalid", Path: collection.Name}
		}
		if collection.Access != "public" && collection.Access != "owner" {
			return &ContractError{Code: "plan.collection_access_invalid", Path: collection.Name}
		}
		if collection.Access == "owner" && backend.Auth != "email_password" {
			return &ContractError{Code: "plan.owner_requires_auth", Path: collection.Name}
		}
		if len(collection.Fields) == 0 || len(collection.Fields) > 50 {
			return &ContractError{Code: "plan.fields_invalid", Path: collection.Name}
		}
		collections[collection.Name] = true
		fields := map[string]bool{}
		for _, field := range collection.Fields {
			if !planIdentifier.MatchString(field.Name) || !boundedText(field.Label, 120) || fields[field.Name] || !validPlanFieldType(field.Type) {
				return &ContractError{Code: "plan.field_invalid", Path: collection.Name + "." + field.Name}
			}
			fields[field.Name] = true
		}
	}
	return nil
}

func validateTextList(values []string, minimum, maximum, textLimit int, code string) error {
	if len(values) < minimum || len(values) > maximum {
		return &ContractError{Code: code}
	}
	for _, value := range values {
		if !boundedText(value, textLimit) {
			return &ContractError{Code: code}
		}
	}
	return nil
}
func boundedText(value string, limit int) bool {
	size := len([]rune(strings.TrimSpace(value)))
	return size > 0 && size <= limit
}
func safePlanPath(value string) bool {
	if !strings.HasPrefix(value, "/src/") || strings.Contains(value, "\\") || strings.ContainsRune(value, 0) || path.Clean(value) != value {
		return false
	}
	extension := path.Ext(value)
	return extension == ".vue" || extension == ".ts" || extension == ".tsx" || extension == ".js" || extension == ".jsx" || extension == ".css" || extension == ".json"
}
func validPlanFieldType(value string) bool {
	return value == "text" || value == "long_text" || value == "number" || value == "boolean" || value == "date"
}
