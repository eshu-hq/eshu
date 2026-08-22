// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type evidenceContentIndex map[string][]indexedEvidenceFile

type indexedEvidenceFile struct {
	path    string
	content string
}

func buildEvidenceContentIndex(envelopes []facts.Envelope) evidenceContentIndex {
	index := make(evidenceContentIndex)
	for _, envelope := range envelopes {
		repoID, filePath, content := envelopeContentIdentity(envelope)
		if repoID == "" || filePath == "" || strings.TrimSpace(content) == "" {
			continue
		}
		index[repoID] = append(index[repoID], indexedEvidenceFile{
			path:    filePath,
			content: content,
		})
	}
	return index
}

func envelopeContentIdentity(envelope facts.Envelope) (string, string, string) {
	// TODO(#4783 W1): fact kind "content" has no typed struct yet (producer
	// go/internal/collector/git_content_fact_envelopes.go emits
	// content_path/content_body/artifact_type). This helper is fact-kind-agnostic
	// — it also serves the typed "file" kind (codegraphv1.File), which carries no
	// outer content field by design — so it cannot route through one decode seam
	// today. Route each caller through its kind's decode seam once the content
	// family lands in sdk/go/factschema.
	filePath, _ := envelope.Payload["relative_path"].(string)
	content, _ := envelope.Payload["content"].(string)

	// The Go collector emits content_path/content_body while some tests and
	// older facts use relative_path/content. Support both payload shapes.
	if filePath == "" {
		filePath, _ = envelope.Payload["content_path"].(string)
	}
	if content == "" {
		content, _ = envelope.Payload["content_body"].(string)
	}

	return sourceRepositoryIDFromEnvelope(envelope), filePath, content
}
