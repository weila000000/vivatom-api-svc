package generation

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"vivatom-api-svc/internal/domain"
)

const (
	PolicyVersion        = "snapshot-guard/v1"
	MaxSnapshotFiles     = 80
	MaxSnapshotFileBytes = 256 * 1024
	MaxSnapshotBytes     = 2 * 1024 * 1024
)

var (
	allowedDependencies = map[string]string{
		"vue": "3.5.42",
	}
	allowedExtensions = map[string]bool{
		".css": true, ".js": true, ".jsx": true, ".json": true,
		".ts": true, ".tsx": true, ".vue": true,
	}
	forbiddenSource = []struct {
		code    string
		pattern *regexp.Regexp
	}{
		{"source.eval", regexp.MustCompile(`\beval\s*\(`)},
		{"source.function_constructor", regexp.MustCompile(`\bFunction\s*\(`)},
		{"source.fetch", regexp.MustCompile(`\bfetch\s*\(`)},
		{"source.xhr", regexp.MustCompile(`\bXMLHttpRequest\b`)},
		{"source.websocket", regexp.MustCompile(`\bWebSocket\b`)},
		{"source.parent_window", regexp.MustCompile(`\bwindow\.(?:parent|top)\b`)},
		{"source.cookie", regexp.MustCompile(`\bdocument\.cookie\b`)},
		{"source.remote_import", regexp.MustCompile(`\bimport\s*\(\s*["']https?://`)},
	}
	remoteCSS = regexp.MustCompile(`(?i)(?:@import|url\s*\()[^;\n]*https?://`)
)

type RejectedError struct {
	Code string
	Path string
}

func (e *RejectedError) Error() string {
	if e.Path == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Path)
}

type Guard struct{}

func NewGuard() Guard {
	return Guard{}
}

func (Guard) Check(input domain.ProjectSnapshot) (domain.ProjectSnapshot, error) {
	if len(input.Files) == 0 {
		return input, &RejectedError{Code: "files.empty"}
	}
	if len(input.Files) > MaxSnapshotFiles {
		return input, &RejectedError{Code: "files.too_many"}
	}
	if !safeSourcePath(input.EntryFile) {
		return input, &RejectedError{Code: "entry.invalid", Path: input.EntryFile}
	}
	if _, exists := input.Files[input.EntryFile]; !exists {
		return input, &RejectedError{Code: "entry.missing", Path: input.EntryFile}
	}

	totalBytes := 0
	files := make(map[string]string, len(input.Files))
	for filePath, source := range input.Files {
		if !safeSourcePath(filePath) {
			return input, &RejectedError{Code: "path.invalid", Path: filePath}
		}
		size := len([]byte(source))
		if size > MaxSnapshotFileBytes {
			return input, &RejectedError{Code: "file.too_large", Path: filePath}
		}
		totalBytes += size
		if totalBytes > MaxSnapshotBytes {
			return input, &RejectedError{Code: "files.too_large"}
		}
		for _, forbidden := range forbiddenSource {
			if forbidden.pattern.MatchString(source) {
				return input, &RejectedError{Code: forbidden.code, Path: filePath}
			}
		}
		if path.Ext(filePath) == ".css" && remoteCSS.MatchString(source) {
			return input, &RejectedError{Code: "source.remote_css", Path: filePath}
		}
		files[filePath] = source
	}

	dependencies := make(map[string]string, len(input.Dependencies))
	for name := range input.Dependencies {
		version, allowed := allowedDependencies[name]
		if !allowed {
			return input, &RejectedError{Code: "dependency.denied", Path: name}
		}
		dependencies[name] = version
	}

	input.Files = files
	input.Dependencies = dependencies
	return input, nil
}

func safeSourcePath(filePath string) bool {
	if !strings.HasPrefix(filePath, "/src/") ||
		strings.Contains(filePath, "\\") ||
		strings.ContainsRune(filePath, 0) ||
		path.Clean(filePath) != filePath {
		return false
	}
	return allowedExtensions[path.Ext(filePath)]
}
