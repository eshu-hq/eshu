// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// relationshipDecodeError wraps a classified contracts-module decode failure
// so relationships read paths can drop malformed evidence without treating a
// zero-value typed payload as valid graph truth.
type relationshipDecodeError struct {
	FactKind       string
	FactID         string
	Field          string
	Classification string
	err            *factschema.DecodeError
}

// Error implements the error interface.
func (e *relationshipDecodeError) Error() string {
	return fmt.Sprintf("decode %s fact %s: %s", e.FactKind, e.FactID, e.err.Error())
}

// Unwrap exposes the classified factschema error to errors.As/errors.Is.
func (e *relationshipDecodeError) Unwrap() error {
	return e.err
}

func newRelationshipDecodeError(factKind, factID string, err error) *relationshipDecodeError {
	var decodeErr *factschema.DecodeError
	if errors.As(err, &decodeErr) {
		return &relationshipDecodeError{
			FactKind:       factKind,
			FactID:         factID,
			Field:          decodeErr.Field,
			Classification: decodeErr.Classification,
			err:            decodeErr,
		}
	}
	return &relationshipDecodeError{
		FactKind:       factKind,
		FactID:         factID,
		Classification: factschema.ClassificationInputInvalid,
		err: &factschema.DecodeError{
			FactKind:       factKind,
			Classification: factschema.ClassificationInputInvalid,
			Err:            err,
		},
	}
}

type relationshipDecodeInput struct {
	FactID        string
	SchemaVersion string
	Payload       map[string]any
}

func relationshipSchemaEnvelope(factKind, schemaVersion string, payload map[string]any) factschema.Envelope {
	if schemaVersion == "" {
		schemaVersion = facts.GCPCloudRelationshipSchemaVersion
	}
	return factschema.Envelope{
		FactKind:      factKind,
		SchemaVersion: schemaVersion,
		Payload:       payload,
	}
}

// decodeGCPCloudRelationship decodes one gcp_cloud_relationship fact row
// through the contracts seam. Missing required identity fields and unsupported
// schema majors yield a classified *relationshipDecodeError.
func decodeGCPCloudRelationship(in relationshipDecodeInput) (gcpv1.Relationship, error) {
	relationship, err := factschema.DecodeGCPCloudRelationship(
		relationshipSchemaEnvelope(factschema.FactKindGCPCloudRelationship, in.SchemaVersion, in.Payload),
	)
	if err != nil {
		return gcpv1.Relationship{}, newRelationshipDecodeError(factschema.FactKindGCPCloudRelationship, in.FactID, err)
	}
	return relationship, nil
}

func decodeGCPCloudRelationshipEnvelope(envelope facts.Envelope) (gcpv1.Relationship, error) {
	return decodeGCPCloudRelationship(relationshipDecodeInput{
		FactID:        envelope.FactID,
		SchemaVersion: envelope.SchemaVersion,
		Payload:       envelope.Payload,
	})
}
