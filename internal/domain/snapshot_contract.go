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
	if strings.TrimSpace(snapshot.Title) == "" {
		return &ContractError{Code: "snapshot.title_missing"}
	}
	if strings.TrimSpace(snapshot.Summary) == "" {
		return &ContractError{Code: "snapshot.summary_missing"}
	}
	for _, planned := range plan.FilePlan {
		if _, exists := snapshot.Files[planned.Path]; !exists {
			return &ContractError{Code: "snapshot.planned_file_missing", Path: planned.Path}
		}
	}
	if !reflect.DeepEqual(normalizeBackend(plan.Backend), normalizeBackend(snapshot.Backend)) {
		return &ContractError{Code: "snapshot.backend_mismatch"}
	}
	return nil
}

func ValidateRevisionContract(previous, candidate ProjectSnapshot) error {
	if !reflect.DeepEqual(normalizeBackend(previous.Backend), normalizeBackend(candidate.Backend)) {
		return &ContractError{Code: "snapshot.backend_changed_without_approval"}
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
