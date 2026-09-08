// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

// RepositoryFactKind is the raw fact-kind literal for a repository fact. It
// has no exported constant in go/internal/facts (the kind is admission-exempt,
// #4752) so the string is spelled out, matching the registry entry's Kind
// value and go/internal/storage/postgres's own repository-kind checks.
const RepositoryFactKind = "repository"

// ContentFactKind is the raw fact-kind literal the git-content collector
// emits (go/internal/collector/git_content_fact_envelopes.go). It has no
// registry entry (#4783 W1): relationships.DiscoverEvidence dispatches
// artifact-type/content evidence off this unregistered kind, not off a typed
// registered one, so Ifá seeds it as a plain string literal too.
const ContentFactKind = "content"

// ContentEntityFactKind and FileFactKind are the internal wire literals the
// git collector emits for a parsed entity and a parsed file
// (go/internal/collector/git_content_fact_envelopes.go, git_fact_builder.go).
// content_entity has no typed payload contract. file does have the public
// codegraph/v1.File contract; catalog fixtures that construct file payloads
// must use that typed struct and factschema.EncodeCodegraphFile before building
// an envelope. These constants remain for older SQL-family dispatch and
// filtering sites that only need the wire kind string.
const (
	ContentEntityFactKind = "content_entity"
	FileFactKind          = "file"
)

// SharedFollowupFactKind is the shared followup wire kind used by the
// repo-dependency fixtures. Exported: repo_dependency_odu.go and
// sql_relationship_odu.go read it from outside this package.
const SharedFollowupFactKind = "shared_followup"
