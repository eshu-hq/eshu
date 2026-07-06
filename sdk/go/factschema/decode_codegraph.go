// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// DecodeCodegraphFile decodes env.Payload into the latest codegraphv1.File
// struct for the "file" fact kind, dispatching on env.SchemaVersion major per
// Contract System v1 §3.2. Callers (reducer handlers) receive either the
// decoded struct or a classified *DecodeError; they must never substitute a
// zero-value struct on error. The returned struct's ParsedFileData field stays
// an untyped map[string]any pass-through — see codegraphv1's package doc for
// why the inner AST shape is intentionally unmodeled (issue #4750).
func DecodeCodegraphFile(env Envelope) (codegraphv1.File, error) {
	return decodeLatestMajor[codegraphv1.File](FactKindCodegraphFile, env)
}

// EncodeCodegraphFile marshals a codegraphv1.File into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeCodegraphFile
// for schema-version-1 payloads, used by this module's round-trip tests.
func EncodeCodegraphFile(file codegraphv1.File) (map[string]any, error) {
	return encodeToPayload(file)
}

// DecodeCodegraphRepository decodes env.Payload into the latest
// codegraphv1.Repository struct for the "repository" fact kind. See
// DecodeCodegraphFile for the dispatch and error contract.
func DecodeCodegraphRepository(env Envelope) (codegraphv1.Repository, error) {
	return decodeLatestMajor[codegraphv1.Repository](FactKindCodegraphRepository, env)
}

// EncodeCodegraphRepository marshals a codegraphv1.Repository into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeCodegraphRepository for schema-version-1 payloads.
func EncodeCodegraphRepository(repository codegraphv1.Repository) (map[string]any, error) {
	return encodeToPayload(repository)
}
