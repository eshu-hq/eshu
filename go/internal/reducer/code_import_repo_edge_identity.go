// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// codeImportFileIdentity carries the decoded "file" fact fields the
// code-import repo-edge builders need: the consumer repository identity, the
// parser-detected language, and the raw parser "imports" value. ParsedFileData
// itself stays untyped past this point — imports is the untyped
// ParsedFileData["imports"] value exactly as the pre-Contract-System code read
// it; callers pass it through mapSlice to get the []map[string]any entries.
type codeImportFileIdentity struct {
	repoID   string
	language string
	imports  any
}

// decodeCodegraphFileImportIdentity decodes one "file" envelope through the
// codegraph contracts seam (decodeCodegraphFile) and extracts the fields the
// code-import repo-edge builders (BuildCodeImportRepoDependencyIntents,
// classifyCodeImportEdges, BuildCodeImportRepoEdgeRefreshIntents, and the
// retract-scope builder in code_import_repo_edge_retract.go) need for their
// join identity. ok is false when the payload is missing a required field
// (repo_id, relative_path, or parsed_file_data) OR when repo_id is present but
// blank/whitespace-only — the caller must skip the fact rather than proceed
// with an empty repository identity, closing the accuracy hole issue #4749
// targets and preserving the pre-Contract-System TrimSpace-then-skip behavior
// these builders had. repoID and language are returned already TrimSpace'd
// (matching the pre-Contract-System payloadStr reads); language stays "" when
// the parser did not detect one (File.Language is optional).
func decodeCodegraphFileImportIdentity(envelope facts.Envelope) (codeImportFileIdentity, bool) {
	file, err := decodeCodegraphFile(envelope)
	if err != nil {
		return codeImportFileIdentity{}, false
	}

	consumerRepoID := strings.TrimSpace(file.RepoID)
	if consumerRepoID == "" {
		return codeImportFileIdentity{}, false
	}

	var language string
	if file.Language != nil {
		language = strings.TrimSpace(*file.Language)
	}

	return codeImportFileIdentity{
		repoID:   consumerRepoID,
		language: language,
		imports:  file.ParsedFileData["imports"],
	}, true
}
