// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"errors"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	servicecatalogv1 "github.com/eshu-hq/eshu/sdk/go/factschema/servicecatalog/v1"
)

// decodeServiceCatalogEntity decodes one service_catalog.entity envelope into
// the typed servicecatalogv1.Entity struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing
// its required entity_ref identity field or is otherwise malformed. It is the
// single decode site for the service_catalog.entity kind's outer envelope on
// the reducer side: the correlation index (service_catalog_correlation_index.go,
// serviceCatalogEntityFromFact) decodes through here for its join identity,
// and a missing entity_ref is routed through partitionDecodeFailures so it
// dead-letters as a per-fact input_invalid quarantine rather than a silent
// empty-string catalog identity (issue #4755).
func decodeServiceCatalogEntity(env facts.Envelope) (servicecatalogv1.Entity, error) {
	entity, err := factschema.DecodeServiceCatalogEntity(factschemaEnvelope(env))
	if err != nil {
		return servicecatalogv1.Entity{}, newFactDecodeError(factschema.FactKindServiceCatalogEntity, err)
	}
	return entity, nil
}

// decodeServiceCatalogOwnership decodes one service_catalog.ownership
// envelope into the typed servicecatalogv1.Ownership struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required entity_ref identity field. It is the single
// decode site for the service_catalog.ownership kind on the reducer side.
func decodeServiceCatalogOwnership(env facts.Envelope) (servicecatalogv1.Ownership, error) {
	ownership, err := factschema.DecodeServiceCatalogOwnership(factschemaEnvelope(env))
	if err != nil {
		return servicecatalogv1.Ownership{}, newFactDecodeError(factschema.FactKindServiceCatalogOwnership, err)
	}
	return ownership, nil
}

// decodeServiceCatalogRepositoryLink decodes one
// service_catalog.repository_link envelope into the typed
// servicecatalogv1.RepositoryLink struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing
// its required entity_ref identity field. It is the single decode site for
// the service_catalog.repository_link kind on the reducer side. A link
// carrying no repository-identifying field (RepositoryID, any URL spelling,
// RepositoryName) still decodes successfully — the reducer's own correlation
// logic classifies that as ServiceCatalogCorrelationRejected, a business
// outcome, not a decode failure.
func decodeServiceCatalogRepositoryLink(env facts.Envelope) (servicecatalogv1.RepositoryLink, error) {
	link, err := factschema.DecodeServiceCatalogRepositoryLink(factschemaEnvelope(env))
	if err != nil {
		return servicecatalogv1.RepositoryLink{}, newFactDecodeError(factschema.FactKindServiceCatalogRepositoryLink, err)
	}
	return link, nil
}

// serviceCatalogDecodeQuarantine builds a visible quarantinedFact from a
// service_catalog decode error that partitionDecodeFailures did NOT classify
// as a per-fact input_invalid (the residual fatal branch — a payload type
// mismatch, or an unsupported schema major). It carries the decode error's own
// classification and field so the malformed fact still surfaces on the
// existing input_invalid counter and structured error log through
// recordQuarantinedFacts, rather than being silently dropped. The field is
// empty when the error is not attributable to a single field. This mirrors
// codegraphDecodeQuarantine (factschema_decode_codegraph.go).
func serviceCatalogDecodeQuarantine(env facts.Envelope, err error) quarantinedFact {
	q := quarantinedFact{
		factID:         env.FactID,
		factKind:       env.FactKind,
		classification: factschema.ClassificationInputInvalid,
	}
	var decodeErr *factschema.DecodeError
	if errors.As(err, &decodeErr) {
		if decodeErr.Classification != "" {
			q.classification = decodeErr.Classification
		}
		q.field = decodeErr.Field
	}
	return q
}
