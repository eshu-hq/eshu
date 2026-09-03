// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/iampolicy"
)

// Local copies of the reducer-root test helpers this family's tests used before
// the move (issue #6061). Go test files cannot share unexported symbols across a
// package boundary, so each helper the moved tests still need is duplicated here
// verbatim rather than exported from the root for test-only use.

// iamEscAccount and iamEscRegion are the fixed scope every fixture principal and
// target share so the in-memory join index resolves them under the trust
// boundary. IAM ARNs are global (empty region) on the AWS side, but the node uid
// folds region, so the fixtures use a stable region to key consistently.
const (
	iamEscAccount = "111122223333"
	iamEscRegion  = ""
)

// attackerUserARN is the fixture principal the CAN_PERFORM tests grant from.
const attackerUserARN = "arn:aws:iam::111122223333:user/attacker"

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

// iamNodeEnvelope builds an aws_resource fact for an IAM node (principal or
// target) so the extractor resolves it to a CloudResource uid through the shared
// join index. The ARN is used as both resource_id and arn so ByARN resolves it.
func iamNodeEnvelope(resourceType, arn string) facts.Envelope {
	return resourceEnvelope(iamEscAccount, iamEscRegion, resourceType, arn, arn)
}

func attackerNode() facts.Envelope {
	return iamNodeEnvelope(iampolicy.ResourceTypeUser, attackerUserARN)
}

func uidOf(resourceType, arn string) string {
	return cloudjoin.CloudResourceUID(iamEscAccount, iamEscRegion, resourceType, arn)
}

// escalationPermissionEnvelope builds one merged aws_iam_permission statement
// fact (actions lowercase, mirroring PR1).
func escalationPermissionEnvelope(
	principalARN, effect string,
	actions, resources []string,
	opts ...func(map[string]any),
) facts.Envelope {
	payload := map[string]any{
		"account_id":           iamEscAccount,
		"region":               iamEscRegion,
		"principal_arn":        principalARN,
		"principal_type":       "user",
		"policy_source":        "inline",
		"effect":               effect,
		"actions":              toAnySlice(actions),
		"not_actions":          []any{},
		"resources":            toAnySlice(resources),
		"not_resources":        []any{},
		"condition_keys":       []any{},
		"assume_principals":    []any{},
		"has_conditions":       false,
		"is_wildcard_action":   containsAny(actions, "*"),
		"is_wildcard_resource": containsAny(resources, "*"),
	}
	for _, opt := range opts {
		opt(payload)
	}
	return facts.Envelope{FactKind: facts.AWSIAMPermissionFactKind, Payload: payload}
}

func withConditions() func(map[string]any) {
	return func(p map[string]any) {
		p["condition_keys"] = []any{"aws:MultiFactorAuthPresent"}
		p["has_conditions"] = true
	}
}

func withNotActions(notActions ...string) func(map[string]any) {
	return func(p map[string]any) { p["not_actions"] = toAnySlice(notActions) }
}

func toAnySlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}

func containsAny(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
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

// allKeyspacesReady reports the requested phase ready for every keyspace, so a
// test can exercise the post-gate path.
func allKeyspacesReady() gpphase.ReadinessLookup {
	return func(_ gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
		return true, true
	}
}

// readyExceptKeyspace reports every keyspace ready except the named one, which
// is reported not-found, so a test can prove the multi-keyspace gate blocks
// until ALL of them commit.
func readyExceptKeyspace(withheld gpphase.Keyspace) gpphase.ReadinessLookup {
	return func(key gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
		if key.Keyspace == withheld {
			return false, false
		}
		return true, true
	}
}

func metricHasAttrs(rm metricdata.ResourceMetrics, metricName string, attrs map[string]string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				matches := true
				for key, want := range attrs {
					got, ok := point.Attributes.Value(attribute.Key(key))
					if !ok || got.AsString() != want {
						matches = false
						break
					}
				}
				if matches && point.Value > 0 {
					return true
				}
			}
		}
	}
	return false
}
