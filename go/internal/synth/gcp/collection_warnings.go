// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// collectionWarningEvery gates how often a synthetic collection warning is
// emitted relative to the generated resource count, keeping the warning
// volume proportional without a warning per resource.
const collectionWarningEvery = 7

// syntheticWarningKinds and syntheticOutcomes are the bounded vocabularies a
// generated collection warning cycles through, matching the collector
// emitter's closed-vocabulary contract (gcpv1.CollectionWarning doc comment)
// without importing collector internals.
var (
	syntheticWarningKinds = []string{"permission_hidden", "quota_exceeded", "stale_snapshot"}
	syntheticOutcomes     = []string{"partial", "unsupported", "stale", "permission-hidden", "quota"}
)

// buildCollectionWarningFacts derives one gcp_collection_warning fact for
// every collectionWarningEvery-th generated resource, so the volume scales
// with ResourceCount without requiring one warning per resource.
func (g *generation) buildCollectionWarningFacts() ([]cassette.Fact, error) {
	var facts []cassette.Fact
	for i, resource := range g.resources {
		if i%collectionWarningEvery != 0 {
			continue
		}
		warning := g.buildOneCollectionWarning(i)

		payload, err := factschema.EncodeGCPCollectionWarning(warning)
		if err != nil {
			return nil, fmt.Errorf("synth/gcp: encode gcp_collection_warning[%d]: %w", i, err)
		}
		fact, err := generateFact(factschema.FactKindGCPCollectionWarning, factKindSchemaVersions[factschema.FactKindGCPCollectionWarning], payload)
		if err != nil {
			return nil, err
		}
		fact.StableFactKey = fmt.Sprintf("gcp:project:%s:warning:%s:%s", g.opts.ProjectID, warning.WarningKind, resource.FullResourceName)
		facts = append(facts, fact)
	}
	return facts, nil
}

// buildOneCollectionWarning synthesizes one schema-valid
// gcpv1.CollectionWarning, cycling deterministically through the bounded
// warning-kind/outcome vocabularies keyed by index.
func (g *generation) buildOneCollectionWarning(index int) gcpv1.CollectionWarning {
	warningKind := syntheticWarningKinds[index%len(syntheticWarningKinds)]
	outcome := syntheticOutcomes[index%len(syntheticOutcomes)]
	reason := fmt.Sprintf("synthetic warning for resource index %d", index)
	retryable := index%2 == 0
	hiddenCount := int64(index % 3)

	return gcpv1.CollectionWarning{
		WarningKind: warningKind,
		Outcome:     outcome,
		Reason:      &reason,
		Retryable:   &retryable,
		HiddenCount: &hiddenCount,
	}
}
