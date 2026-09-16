package domain

import (
	"fmt"
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
	return nil
}

func ValidateRevisionContract(previous, candidate ProjectSnapshot) error {
	if err := validateSnapshotMetadata(candidate); err != nil {
		return err
	}
	return nil
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
