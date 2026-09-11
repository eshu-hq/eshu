// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Phase orders when a per-language [Resolver] runs relative to the generic
// repo-unique-name fallback.
type Phase string

const (
	// PhaseBeforeRepoFallback runs before the generic repo-unique-name
	// fallback, so a confident language-specific match wins over a broad guess.
	PhaseBeforeRepoFallback Phase = "before_repo_fallback"
	// PhaseAfterRepoFallback runs after the generic repo-unique-name fallback,
	// for a language-specific match that should only be tried once the
	// generic paths are exhausted.
	PhaseAfterRepoFallback Phase = "after_repo_fallback"
)

// Resolver pairs a language-specific resolve function with the [Phase] it
// runs in.
type Resolver struct {
	Phase   Phase
	Resolve func(ResolveContext) (string, string, codeprovenance.Method)
}

// ResolveContext carries the per-call state a language resolver needs: the
// built [EntityIndex], the caller's repository and import state, the raw call
// metadata, and the resolved language string. Every field is exported because
// every language leaf's resolver reads it directly.
type ResolveContext struct {
	Index             EntityIndex
	RepositoryID      string
	RepositoryImports map[string][]string
	ReexportIndex     ReexportIndex
	RawPath           string
	RelativePath      string
	FileData          map[string]any
	Call              map[string]any
	Language          string
}

// CallName returns the trimmed "name" field of the call/edge metadata.
func (c ResolveContext) CallName() string {
	return strings.TrimSpace(payloadcore.AnyToString(c.Call["name"]))
}
