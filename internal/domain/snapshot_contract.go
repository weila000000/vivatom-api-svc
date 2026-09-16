package domain

import (
	"fmt"
	"reflect"
	"strings"
)

type ContractError struct {
	Code string
	Path string
}

func (e *ContractError) Error() string {
	if e.Path == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Path)
}

func ValidateBuildContract(plan BuildPlan, snapshot ProjectSnapshot) error {
	if err := validateSnapshotMetadata(snapshot); err != nil {
		return err
	}
	for _, planned := range plan.FilePlan {
		if _, exists := snapshot.Files[planned.Path]; !exists {
			return &ContractError{Code: "snapshot.planned_file_missing", Path: planned.Path}
		}
	}
	if !reflect.DeepEqual(normalizeBackend(plan.Backend), normalizeBackend(snapshot.Backend)) {
		return &ContractError{Code: "snapshot.backend_mismatch"}
	}
	if err := validateRuntimeIntegration(snapshot); err != nil {
		return err
	}
	return nil
}

func ValidateRevisionContract(previous, candidate ProjectSnapshot) error {
	if err := validateSnapshotMetadata(candidate); err != nil {
		return err
	}
	if !reflect.DeepEqual(normalizeBackend(previous.Backend), normalizeBackend(candidate.Backend)) {
		return &ContractError{Code: "snapshot.backend_changed_without_approval"}
	}
	if err := validateRuntimeIntegration(candidate); err != nil {
		return err
	}
	return nil
}

func validateRuntimeIntegration(snapshot ProjectSnapshot) error {
	if !snapshot.Backend.Enabled {
		return nil
	}
	if _, exists := snapshot.Files["/src/vivatom-runtime.ts"]; !exists {
		return &ContractError{Code: "snapshot.runtime_sdk_missing"}
	}
	for path, source := range snapshot.Files {
		if path != "/src/vivatom-runtime.ts" && strings.Contains(source, "vivatom-runtime") {
			return nil
		}
	}
	return &ContractError{Code: "snapshot.runtime_not_integrated"}
}

func validateSnapshotMetadata(snapshot ProjectSnapshot) error {
	titleLength := len([]rune(strings.TrimSpace(snapshot.Title)))
	if titleLength == 0 {
		return &ContractError{Code: "snapshot.title_missing"}
	}
	if titleLength > 200 {
		return &ContractError{Code: "snapshot.title_too_long"}
	}
	summaryLength := len([]rune(strings.TrimSpace(snapshot.Summary)))
	if summaryLength == 0 {
		return &ContractError{Code: "snapshot.summary_missing"}
	}
	if summaryLength > 2000 {
		return &ContractError{Code: "snapshot.summary_too_long"}
	}
	return nil
}

func normalizeBackend(value BackendSpec) BackendSpec {
	if value.Collections == nil {
		value.Collections = []BackendCollection{}
	}
	for index := range value.Collections {
		if value.Collections[index].Fields == nil {
			value.Collections[index].Fields = []BackendField{}
		}
	}
	return value
}
