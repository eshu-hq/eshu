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
// identifier used to be built from the workload name alone, so two
// repositories with a same-named workload produced the same id and collapsed
// onto one node (#5385); the constructors now emit the repository-scoped key,
// and nothing outside this package can mint an id on another format.
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
// The format is repository-scoped, workload:<repo_id>:<name> (#5385 Option C):
// two repositories with a same-named workload produce distinct ids, so their
// nodes, DEFINES edges, and instances no longer collapse onto one node.
//
// A blank repository or workload name yields the empty id rather than an
// identifier with an empty segment, because that segment would MERGE every
// candidate missing it onto one shared node. A colon inside the workload name
// is rejected for the same fail-closed reason: the composite has no parser,
// so a colon-bearing name would make the key ambiguous to a human reader
// (compatibility doc decision "Colon rejection"). The repository segment is
// exempt: canonical repo ids carry their own prefix. Callers drop the empty
// id.
func NewWorkloadID(repoID, workloadName string) WorkloadID {
	repo := strings.TrimSpace(repoID)
	name := strings.TrimSpace(workloadName)
	if repo == "" || name == "" || strings.Contains(name, ":") {
		return WorkloadID{}
	}
	return WorkloadID{v: fmt.Sprintf("workload:%s:%s", repo, name)}
}

// NewWorkloadInstanceID builds the canonical WorkloadInstance identifier.
//
// The format derives from the scoped workload key:
// workload-instance:<repo_id>:<name>:<environment>. A blank repository,
// workload name, or environment yields the empty id rather than an identifier
// with an empty segment, which would collide across every candidate missing
// that segment.
func NewWorkloadInstanceID(repoID, workloadName, environment string) WorkloadInstanceID {
	repo := strings.TrimSpace(repoID)
	name := strings.TrimSpace(workloadName)
	env := strings.TrimSpace(environment)
	if repo == "" || name == "" || env == "" || strings.Contains(name, ":") || strings.Contains(env, ":") {
		return WorkloadInstanceID{}
	}
	return WorkloadInstanceID{v: fmt.Sprintf("workload-instance:%s:%s:%s", repo, name, env)}
}

// String returns the identifier as a plain string, for the Cypher parameters,
// row builders and struct fields that still take one. Converting those
// consumers is deliberately not part of this step: this step only makes
// construction enumerable, and changes no identifier value.
func (id WorkloadID) String() string { return id.v }

// RejectionReason is the bounded reason a candidate's inputs cannot mint a
// repository-scoped identity. The values match the reason label on
// eshu_dp_workload_identity_rejected_total
// (docs/internal/design/5385-workload-identity-migration.md): a drop is
// counted, never silent (#5385).
type RejectionReason string

const (
	// RejectionBlankSegment counts a blank repository, workload name, or
	// environment segment.
	RejectionBlankSegment RejectionReason = "blank_segment"
	// RejectionColonInName counts a colon inside the workload name, which
	// would make the composite key ambiguous to a human reader.
	RejectionColonInName RejectionReason = "colon_in_name"
	// RejectionColonInEnvironment counts a colon inside the environment
	// segment of a workload-instance identity.
	RejectionColonInEnvironment RejectionReason = "colon_in_environment"
)

// RejectionTally counts identity rejections by reason for one projection
// build. The zero value is ready to use; a nil tally discards every note so
// read-only rebuilds of the same rows (keep-lists, Ifa edges) share the
// builders without double-counting the write path's emission.
type RejectionTally struct {
	BlankSegment       int
	ColonInName        int
	ColonInEnvironment int
}

// Note counts one rejection. Nil-safe: a nil tally drops the note.
func (t *RejectionTally) Note(reason RejectionReason) {
	if t == nil {
		return
	}
	switch reason {
	case RejectionBlankSegment:
		t.BlankSegment++
	case RejectionColonInName:
		t.ColonInName++
	case RejectionColonInEnvironment:
		t.ColonInEnvironment++
	}
}

// Empty reports whether no rejection was noted.
func (t RejectionTally) Empty() bool {
	return t.BlankSegment == 0 && t.ColonInName == 0 && t.ColonInEnvironment == 0
}

// ClassifyWorkloadRejection reports why the workload constructor returned the
// empty id for these inputs. Call it only then: with a non-blank repository
// and name the only remaining rejection is a colon in the name.
func ClassifyWorkloadRejection(repoID, workloadName string) RejectionReason {
	if strings.TrimSpace(repoID) == "" || strings.TrimSpace(workloadName) == "" {
		return RejectionBlankSegment
	}
	return RejectionColonInName
}

// ClassifyInstanceRejection reports why the workload-instance constructor
// returned the empty id for these inputs. Call it only then.
func ClassifyInstanceRejection(repoID, workloadName, environment string) RejectionReason {
	if strings.TrimSpace(repoID) == "" || strings.TrimSpace(workloadName) == "" ||
		strings.TrimSpace(environment) == "" {
		return RejectionBlankSegment
	}
	if strings.Contains(workloadName, ":") {
		return RejectionColonInName
	}
	return RejectionColonInEnvironment
}

// String returns the identifier as a plain string. See WorkloadID.String.
func (id WorkloadInstanceID) String() string { return id.v }
