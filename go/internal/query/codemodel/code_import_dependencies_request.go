// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"fmt"
	"strings"
)

// ImportDependencyRequest is the decoded import-dependency investigation
// request. It split here from root code_import_dependencies.go (#6060 lane
// A L1): all seven builders take one, and Go requires a type's methods to
// live with its declaration, so the request's methods move with it. The
// *CodeHandler route methods stay in root; root's family_code_shim.go
// aliases this type back so the staying handler, executors, and tests keep
// their names. Validate, EffectiveQueryType, NormalizedLanguage, and
// QueryLimit are exported because staying root callers use them;
// hasScopeFilter and normalizedLimit serve only this package.
//
// It moved here from code_import_dependencies_queries.go when that file hit
// the 500-line cap (#7347/#7348): builders stay there, the request family
// lives here. Same package, no behavior change, no symbol renamed or
// exported.
//
// Access carries the caller's repository grant, set by the handler from
// the request's AuthContext and never decoded from the body: the json:"-"
// tag preserves the unexported field's old decode behavior now that the
// field must be exported for staying root constructors to set it.
type ImportDependencyRequest struct {
	QueryType    string `json:"query_type"`
	RepoID       string `json:"repo_id"`
	Language     string `json:"language"`
	SourceFile   string `json:"source_file"`
	TargetFile   string `json:"target_file"`
	SourceModule string `json:"source_module"`
	TargetModule string `json:"target_module"`
	Limit        int    `json:"limit"`
	Offset       int    `json:"offset"`
	// MaxCycleLength bounds file_import_cycles simple-cycle enumeration.
	// Zero means the default; it is ignored by every other query type.
	MaxCycleLength int `json:"max_cycle_length"`

	Access repositoryAccessFilter `json:"-"`
}

const (
	importDependencyDefaultLimit = 25
	importDependencyMaxLimit     = 200
	importDependencyMaxOffset    = 10000
)

// Validate rejects an unbounded or malformed investigation request before
// any graph read runs.
func (r ImportDependencyRequest) Validate() error {
	if _, ok := importDependencyQueryTypes()[r.EffectiveQueryType()]; !ok {
		return fmt.Errorf("query_type must be one of: %s", strings.Join(importDependencyQueryTypeNames(), ", "))
	}
	if r.Limit > importDependencyMaxLimit {
		return fmt.Errorf("limit must be <= 200")
	}
	if r.Limit < 0 {
		return fmt.Errorf("limit must be >= 0")
	}
	if r.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	if r.Offset > importDependencyMaxOffset {
		return fmt.Errorf("offset must be <= 10000")
	}
	if !r.hasScopeFilter() {
		return fmt.Errorf("one of repo_id, source_file, target_file, source_module, or target_module is required")
	}
	if strings.TrimSpace(r.TargetFile) != "" && r.EffectiveQueryType() != "file_import_cycles" && r.EffectiveQueryType() != "cross_module_calls" {
		return fmt.Errorf("target_file is supported only for file_import_cycles and cross_module_calls")
	}
	if r.EffectiveQueryType() == "file_import_cycles" {
		language := r.NormalizedLanguage()
		if language != "" && language != "python" && language != "go" && !isTSCycleLanguage(language) {
			return fmt.Errorf("file_import_cycles currently supports python module-name, go package, and typescript/tsx/javascript package cycle detection")
		}
		if r.MaxCycleLength != 0 && (r.MaxCycleLength < importCycleMinMaxLength || r.MaxCycleLength > importCycleMaxMaxLength) {
			return fmt.Errorf(
				"max_cycle_length must be between %d and %d",
				importCycleMinMaxLength,
				importCycleMaxMaxLength,
			)
		}
	}
	return nil
}

// EffectiveQueryType normalizes the requested investigation family,
// defaulting to the per-file import listing. It cannot be named QueryType:
// the request struct already has a QueryType field carrying the raw decoded
// value.
func (r ImportDependencyRequest) EffectiveQueryType() string {
	queryType := strings.ToLower(strings.TrimSpace(r.QueryType))
	if queryType == "" {
		return "imports_by_file"
	}
	return queryType
}

// NormalizedLanguage lowercases the requested language filter.
func (r ImportDependencyRequest) NormalizedLanguage() string {
	return strings.ToLower(strings.TrimSpace(r.Language))
}

// isTSCycleLanguage reports whether a normalized language is one the
// TypeScript/JavaScript package lift answers (#7348): typescript, tsx, and
// javascript files collapse into their owning npm packages.
func isTSCycleLanguage(language string) bool {
	switch language {
	case "typescript", "tsx", "javascript":
		return true
	default:
		return false
	}
}

// EffectiveCycleLanguage resolves the language one file_import_cycles read
// runs in: the caller's filter when set, python otherwise. An unfiltered
// request stays Python-only, exactly as before the package lifts
// (#7347/#7348). The
// edge fetch and the row decoder share this, so the graph never returns a
// language the decoder would drop. Exported because the codequery reader
// binds the $cycle_language parameter from it.
func (r ImportDependencyRequest) EffectiveCycleLanguage() string {
	if language := r.NormalizedLanguage(); language != "" {
		return language
	}
	return "python"
}

// effectiveMaxCycleLength resolves the file_import_cycles simple-cycle
// length bound: the caller's max_cycle_length when set and valid, the
// default otherwise. Validation rejects out-of-range values before any
// graph read runs, so this resolves rather than clamps.
func (r ImportDependencyRequest) effectiveMaxCycleLength() int {
	if r.MaxCycleLength <= 0 {
		return importCycleDefaultMaxLength
	}
	return r.MaxCycleLength
}

func (r ImportDependencyRequest) normalizedLimit() int {
	switch {
	case r.Limit <= 0:
		return importDependencyDefaultLimit
	case r.Limit > importDependencyMaxLimit:
		return importDependencyMaxLimit
	default:
		return r.Limit
	}
}

// QueryLimit bounds one investigation page with a limit+1 truncation probe.
func (r ImportDependencyRequest) QueryLimit() int {
	return r.normalizedLimit() + 1
}

func (r ImportDependencyRequest) hasScopeFilter() bool {
	for _, value := range []string{r.RepoID, r.SourceFile, r.TargetFile, r.SourceModule, r.TargetModule} {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func importDependencyQueryTypes() map[string]struct{} {
	return map[string]struct{}{
		"imports_by_file":     {},
		"importers":           {},
		"module_dependencies": {},
		"package_imports":     {},
		"file_import_cycles":  {},
		"cross_module_calls":  {},
	}
}

func importDependencyQueryTypeNames() []string {
	return []string{"imports_by_file", "importers", "module_dependencies", "package_imports", "file_import_cycles", "cross_module_calls"}
}
