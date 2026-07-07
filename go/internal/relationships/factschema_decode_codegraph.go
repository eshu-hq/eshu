// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

func codegraphSchemaEnvelope(factKind, schemaVersion string, payload map[string]any) factschema.Envelope {
	if schemaVersion == "" {
		schemaVersion = "1.0.0"
	}
	return factschema.Envelope{
		FactKind:      factKind,
		SchemaVersion: schemaVersion,
		Payload:       payload,
	}
}

// decodeCodegraphFile decodes one file fact through the contracts seam. Its
// ParsedFileData stays intentionally opaque; this validates the outer identity
// fields before relationships read the inner parser payload.
func decodeCodegraphFile(in relationshipDecodeInput) (codegraphv1.File, error) {
	file, err := factschema.DecodeCodegraphFile(
		codegraphSchemaEnvelope(factschema.FactKindCodegraphFile, in.SchemaVersion, in.Payload),
	)
	if err != nil {
		return codegraphv1.File{}, newRelationshipDecodeError(factschema.FactKindCodegraphFile, in.FactID, err)
	}
	return file, nil
}

func decodeCodegraphFileEnvelope(envelope facts.Envelope) (codegraphv1.File, error) {
	return decodeCodegraphFile(relationshipDecodeInput{
		FactID:        envelope.FactID,
		SchemaVersion: envelope.SchemaVersion,
		Payload:       envelope.Payload,
	})
}
