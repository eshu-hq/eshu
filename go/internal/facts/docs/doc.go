// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package docs declares the documentation fact family: its fact kinds and
// schema versions, the Go payload structs a collector fills, the stable-id
// derivations that give each payload its durable graph identity, the bounded
// source-ACL state vocabulary, and the Encode* functions that map each
// payload onto its sdk/go/factschema documentation/v1 encoder.
//
// The family moved out of go/internal/facts in issue #6776 and its exported
// names lost the redundant Documentation prefix (docs/internal/naming.md
// rule 4): what the facts root spelled DocumentationSourcePayload is
// [SourcePayload] here, and EncodeDocumentationSection is [EncodeSection].
// Every pre-move spelling is still reachable as facts.Documentation* through
// the root's transitional compat_docs.go, which aliases these declarations
// rather than redefining them, so the two spellings are the same type and
// the same value.
//
// The package is named docs, not documentation, because go/build excludes
// every .go file in a package literally named documentation from the build
// (go/build/build.go treats it as documentation-only source), so a package
// named documentation compiles nowhere and reports "build constraints
// exclude all Go files".
//
// # Identity is a frozen contract
//
// [SourceStableID], [DocumentStableID], [SectionStableID], [LinkStableID],
// [EntityMentionStableID], [ClaimCandidateStableID], [FindingStableID], and
// [EvidencePacketStableID] each hash a deliberately durable subset of their
// payload through encode.StableID. A section's id, for example, is derived
// from the document, revision, and section identity and never from its
// heading text or rendered content, so re-reading an edited page does not
// mint a second node for the same section. Changing what a derivation hashes
// orphans every node already written under the old id; it is a breaking
// change, not a refactor.
//
// # What this package does not do
//
// It performs no I/O, holds no collector or reducer logic, and admits
// nothing. The Encode* functions are pure payload mapping: they return the
// error their factschema encoder returns and normalize the result to the
// shape a JSON round-trip produces, so an in-process payload and one read
// back from Postgres compare equal. [EncodeFinding] and [EncodeEvidencePacket]
// take an untyped map because the documentation verifier owns extension
// fields the typed contract intentionally leaves open; they preserve exactly
// the named open fields and drop nothing else silently.
//
// # Import direction
//
// This package must never import go/internal/facts. The facts root imports
// the nested families to build its schemaVersionFamilies table, so the
// reverse edge is an import cycle. Shared payload substrate lives in
// go/internal/facts/encode for that reason.
package docs
