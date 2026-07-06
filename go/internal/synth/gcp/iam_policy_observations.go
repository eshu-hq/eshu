// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// iamObservationEvery gates how often a synthetic IAM policy observation is
// emitted relative to the generated resource count.
const iamObservationEvery = 5

// syntheticRoles is the bounded IAM role vocabulary a generated observation
// cycles through.
var syntheticRoles = []string{"roles/storage.admin", "roles/viewer", "roles/editor", "roles/owner"}

// buildIAMPolicyObservationFacts derives one gcp_iam_policy_observation fact
// for every iamObservationEvery-th generated resource, binding it to that
// resource's real FullResourceName and AssetType.
func (g *generation) buildIAMPolicyObservationFacts() ([]cassette.Fact, error) {
	var facts []cassette.Fact
	for i, resource := range g.resources {
		if i%iamObservationEvery != 0 {
			continue
		}
		observation := g.buildOneIAMPolicyObservation(i, resource)

		payload, err := factschema.EncodeGCPIAMPolicyObservation(observation)
		if err != nil {
			return nil, fmt.Errorf("synth/gcp: encode gcp_iam_policy_observation[%d]: %w", i, err)
		}
		fact, err := generateFact(factschema.FactKindGCPIAMPolicyObservation, factKindSchemaVersions[factschema.FactKindGCPIAMPolicyObservation], payload)
		if err != nil {
			return nil, err
		}
		fact.StableFactKey = fmt.Sprintf("gcp:project:%s:iam:%s:%s", g.opts.ProjectID, observation.Role, resource.FullResourceName)
		facts = append(facts, fact)
	}
	return facts, nil
}

// buildOneIAMPolicyObservation synthesizes one schema-valid
// gcpv1.IAMPolicyObservation bound to resource. Members carries fingerprinted
// member evidence only (matching the real emitter's no-raw-identity
// contract) and is never empty, satisfying the schema's required,
// non-emptiness-enforced-by-the-emitter Members contract.
func (g *generation) buildOneIAMPolicyObservation(index int, resource gcpv1.Resource) gcpv1.IAMPolicyObservation {
	role := syntheticRoles[index%len(syntheticRoles)]
	projectID := g.opts.ProjectID
	conditionPresent := index%2 == 0
	var conditionFingerprint *string
	if conditionPresent {
		f := fingerprint(fmt.Sprintf("condition-%d", index))
		conditionFingerprint = &f
	}
	etagFingerprint := fingerprint(fmt.Sprintf("etag-%d", index))

	return gcpv1.IAMPolicyObservation{
		FullResourceName: resource.FullResourceName,
		AssetType:        resource.AssetType,
		Role:             role,
		ProjectID:        &projectID,
		Members: []map[string]string{
			{
				"member_class":       "user",
				"member_fingerprint": fingerprint(fmt.Sprintf("member-%d", index)),
			},
		},
		ConditionPresent:     &conditionPresent,
		ConditionFingerprint: conditionFingerprint,
		EtagFingerprint:      &etagFingerprint,
	}
}
