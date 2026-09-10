// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package quality

import (
	"strings"
)

// Capability is the contract capability for code-quality inspection.
const Capability = "code_quality.refactoring"

// Check names for the supported inspections.
const (
	CheckComplex  = "complexity"
	CheckLength   = "function_length"
	CheckArgs     = "argument_count"
	CheckRefactor = "refactoring_candidates"
)

// Request and paging bounds for one inspection.
const (
	DefaultLimit = 10
	MaxLimit     = 100
	MaxOffset    = 10000
)

// Default metric thresholds for one inspection.
const (
	DefaultLines      = 20
	DefaultArgs       = 5
	DefaultComplexity = 10
)

// Request describes one code-quality inspection: a check over a
// repository, language, entity, or function name, with metric floors
// and paging. It decodes from the route's JSON body unchanged from its
// codequery home.
type Request struct {
	Check         string `json:"check"`
	RepoID        string `json:"repo_id"`
	Language      string `json:"language"`
	EntityID      string `json:"entity_id"`
	FunctionName  string `json:"function_name"`
	MinComplexity int    `json:"min_complexity"`
	MinLines      int    `json:"min_lines"`
	MinArguments  int    `json:"min_arguments"`
	Limit         int    `json:"limit"`
	Offset        int    `json:"offset"`
}

// Normalize trims the selectors, defaults an empty check to the
// refactoring-candidates sweep, and floors the metric thresholds and
// paging to the handler's bounds.
func (r *Request) Normalize() {
	r.Check = strings.TrimSpace(r.Check)
	if r.Check == "" {
		r.Check = CheckRefactor
	}
	r.RepoID = strings.TrimSpace(r.RepoID)
	r.Language = strings.TrimSpace(r.Language)
	r.EntityID = strings.TrimSpace(r.EntityID)
	r.FunctionName = strings.TrimSpace(r.FunctionName)
	r.Limit = NormalizeLimit(r.Limit)
	if r.Offset < 0 {
		r.Offset = 0
	}
	if r.MinLines <= 0 {
		r.MinLines = DefaultLines
	}
	if r.MinArguments <= 0 {
		r.MinArguments = DefaultArgs
	}
	if r.MinComplexity <= 0 {
		if r.Check == CheckComplex {
			r.MinComplexity = 1
		} else {
			r.MinComplexity = DefaultComplexity
		}
	}
}

// SupportedCheck reports whether check names a supported inspection.
func SupportedCheck(check string) bool {
	switch check {
	case CheckComplex, CheckLength, CheckArgs, CheckRefactor:
		return true
	default:
		return false
	}
}

// NormalizeLimit floors and caps a result limit to the handler's
// bounds.
func NormalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}
