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
	filePayload, ok := evidenceFilePayloadFromEnvelope(envelope)
	if !ok {
		return "", "", ""
	}
	return filePayload.sourceRepoID, filePayload.filePath, filePayload.content
}

func legacyEnvelopeContentIdentity(envelope facts.Envelope) (string, string, string) {
	filePath, _ := envelope.Payload["relative_path"].(string)
	content := envelopeContentBody(envelope.Payload)

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

func envelopeContentBody(payload map[string]any) string {
	content, _ := payload["content"].(string)
	if content == "" {
		content, _ = payload["content_body"].(string)
	}
	return content
}
