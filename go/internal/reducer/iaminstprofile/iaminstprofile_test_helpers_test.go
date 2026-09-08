// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iaminstprofile

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// Local copies of the reducer-root test helpers this family's tests used before
// the move (issue #6061). Go test files cannot share unexported symbols across a
// package boundary, so each helper the moved tests still need is duplicated here
// verbatim rather than exported from the root for test-only use.

func awsResourceEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSResourceFactKind,
		Payload:  payload,
	}
}

// resourceEnvelope is a small helper for join-index tests. account+region are
// part of the uid identity (the cross-account/region trust boundary).
func resourceEnvelope(accountID, region, resourceType, resourceID, arn string, anchors ...string) facts.Envelope {
	anchorVals := make([]any, 0, len(anchors))
	for _, a := range anchors {
		anchorVals = append(anchorVals, a)
	}
	return awsResourceEnvelope(map[string]any{
		"account_id":          accountID,
		"region":              region,
		"resource_type":       resourceType,
		"resource_id":         resourceID,
		"arn":                 arn,
		"correlation_anchors": anchorVals,
	})
}

// iamInstanceProfileResourceEnvelope builds an aws_iam_instance_profile
// aws_resource envelope with the same nested-attributes shape the real
// awscloud IAM scanner emits (awscloud.NewResourceEnvelope ->
// awsPayloadAttributes flattens the scanner's service-specific attributes,
// including role_arns, under one top-level "attributes" key rather than at
// the payload's top level; see #4633).
func iamInstanceProfileResourceEnvelope(accountID, profileName string, roleARNs ...string) facts.Envelope {
	profileARN := "arn:aws:iam::" + accountID + ":instance-profile/" + profileName
	roles := make([]any, 0, len(roleARNs))
	for _, arn := range roleARNs {
		roles = append(roles, arn)
	}
	return facts.Envelope{
		FactKind: facts.AWSResourceFactKind,
		Payload: map[string]any{
			"account_id":          accountID,
			"region":              "aws-global",
			"resource_type":       "aws_iam_instance_profile",
			"resource_id":         profileARN,
			"arn":                 profileARN,
			"name":                profileName,
			"correlation_anchors": []any{profileARN, profileName},
			"attributes": map[string]any{
				"collector_instance_id": "test-instance",
				"role_arns":             roles,
			},
		},
	}
}

func iamInstanceProfileUID(accountID, profileName string) string {
	arn := "arn:aws:iam::" + accountID + ":instance-profile/" + profileName
	return cloudjoin.CloudResourceUID(accountID, "aws-global", "aws_iam_instance_profile", arn)
}

func iamRoleUID(accountID, roleName string) string {
	arn := "arn:aws:iam::" + accountID + ":role/" + roleName
	return cloudjoin.CloudResourceUID(accountID, "aws-global", "aws_iam_role", arn)
}

// iamRoleEnvelope builds the aws_resource node fact an IAM role resolves
// through the shared join index. IAM is a global service: region is
// "aws-global" and resource_id == arn, matching the iam scanner's
// roleObservation. It used to live beside the CAN_ASSUME edge-row tests, which
// moved to internal/reducer/iamcan in #6061; Go test files cannot share
// unexported symbols across a package boundary, so this copy lives here for
// the instance-profile-role tests.
func iamRoleEnvelope(accountID, arn string) facts.Envelope {
	return resourceEnvelope(accountID, "aws-global", "aws_iam_role", arn, arn, arn)
}

// stubFactLoader replays a fixed envelope batch and counts loads.
type stubFactLoader struct {
	envelopes []facts.Envelope
	calls     int
}

func (f *stubFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	f.calls++
	return f.envelopes, nil
}

func readyLookup(ready, found bool) gpphase.ReadinessLookup {
	return func(_ gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
		return ready, found
	}
}

func iamInstanceProfileRoleIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-profile-role-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducercontract.DomainIAMInstanceProfileRoleMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func iamInstanceProfileRoleFixture() []facts.Envelope {
	const acct = "123456789012"
	roleARN := "arn:aws:iam::" + acct + ":role/app"
	return []facts.Envelope{
		iamInstanceProfileResourceEnvelope(acct, "app-profile", roleARN),
		iamRoleEnvelope(acct, roleARN),
	}
}
