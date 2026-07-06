// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	documentationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/documentation/v1"
)

// decodeDocumentationDocument decodes one documentation_document envelope
// into the typed documentationv1.Document struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing
// its required document_id field or is otherwise malformed. It is the single
// decode site for the documentation_document kind on the reducer side:
// buildDocumentationDeltaScope decodes through here, and a missing required
// field is routed through partitionDecodeFailures so it dead-letters as a
// per-fact input_invalid quarantine rather than a silent drop from delta
// tracking.
func decodeDocumentationDocument(env facts.Envelope) (documentationv1.Document, error) {
	document, err := factschema.DecodeDocumentationDocument(factschemaEnvelope(env))
	if err != nil {
		return documentationv1.Document{}, newFactDecodeError(factschema.FactKindDocumentationDocument, err)
	}
	return document, nil
}

// decodeDocumentationEntityMention decodes one documentation_entity_mention
// envelope into the typed documentationv1.EntityMention struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required document_id, section_id, or
// resolution_status field, or is otherwise malformed. It is the single
// decode site for the documentation_entity_mention kind on the reducer side:
// ExtractDocumentationEdgeRows decodes through here, and a missing required
// field is routed through partitionDecodeFailures so it dead-letters as a
// per-fact input_invalid quarantine rather than the mention silently being
// skipped with no operator signal.
func decodeDocumentationEntityMention(env facts.Envelope) (documentationv1.EntityMention, error) {
	mention, err := factschema.DecodeDocumentationEntityMention(factschemaEnvelope(env))
	if err != nil {
		return documentationv1.EntityMention{}, newFactDecodeError(factschema.FactKindDocumentationEntityMention, err)
	}
	return mention, nil
}
