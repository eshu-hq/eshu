// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadid

import (
	"fmt"
	"strings"
)

// WorkloadID is the canonical graph identifier for a Workload node.
//
// It exists as a distinct type so the compiler, rather than text search, can
// enumerate every place the identifier is constructed. That matters because the
// identifier is due to change: it is currently built from the workload name
// alone, so two repositories with a same-named workload produce the same id and
// collapse onto one node (#5385).
//
// Section 4 of docs/internal/design/5385-workload-identity-key.md is a
// hand-maintained inventory of those places, and it has been wrong three times.
// Text search does find the construction sites; what it does not do is keep a
// hand-maintained list of them correct as the tree moves, which is the failure
// this type removes. Nothing outside this file can produce a WorkloadID without
// the compiler saying so.
//
// The representation is opaque on purpose: the field is unexported, so the
// only way to mint a WorkloadID is a constructor in this package. A
// hand-built conversion such as WorkloadID("workload:" + name) does not
// compile, which means the re-key cannot silently leave a caller behind on
// the old format while the constructors move (#6580 codex P1). Callers that
// still assemble "workload:" + name by hand as a plain string are outside
// its reach -- see the package README for the ones that remain and why they
// are the re-key's work rather than this package's.
type WorkloadID struct {
	// v is the identifier text.
	v string
}

// WorkloadInstanceID is the canonical graph identifier for a WorkloadInstance
// node. It carries the same collision defect as WorkloadID and exists for the
// same reason: to make construction sites enumerable by the compiler. It is
// opaque for the same reason as WorkloadID.
type WorkloadInstanceID struct {
	// v is the identifier text.
	v string
}

// NewWorkloadID builds the canonical Workload identifier.
//
// repoID is accepted but deliberately unused: the current format is name-only,
// and that is precisely the defect #5385 describes. Taking the repository at
// every call site now means the re-key becomes a change inside this function
// rather than a change at every caller, which is the whole reason to do this
// before the format moves.
//
// A blank workload name yields the empty id rather than a bare "workload:"
// prefix, because that prefix would MERGE every blank-named candidate onto one
// shared node.
func NewWorkloadID(repoID, workloadName string) WorkloadID {
	_ = repoID // reserved for the repository-scoped key; see #5385.
	name := strings.TrimSpace(workloadName)
	if name == "" {
		return WorkloadID{}
	}
	return WorkloadID{v: fmt.Sprintf("workload:%s", name)}
}

// NewWorkloadInstanceID builds the canonical WorkloadInstance identifier.
//
// repoID is accepted and unused for the same reason as NewWorkloadID. A blank
// workload name or environment yields the empty id rather than an identifier
// with an empty segment, which would collide across every candidate missing
// that segment.
func NewWorkloadInstanceID(repoID, workloadName, environment string) WorkloadInstanceID {
	_ = repoID // reserved for the repository-scoped key; see #5385.
	name := strings.TrimSpace(workloadName)
	env := strings.TrimSpace(environment)
	if name == "" || env == "" {
		return WorkloadInstanceID{}
	}
	return WorkloadInstanceID{v: fmt.Sprintf("workload-instance:%s:%s", name, env)}
}

// String returns the identifier as a plain string, for the Cypher parameters,
// row builders and struct fields that still take one. Converting those
// consumers is deliberately not part of this step: this step only makes
// construction enumerable, and changes no identifier value.
func (id WorkloadID) String() string { return id.v }

// String returns the identifier as a plain string. See WorkloadID.String.
func (id WorkloadInstanceID) String() string { return id.v }
