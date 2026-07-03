// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	s3InternetExposureStateExposed    = "exposed"
	s3InternetExposureStateNotExposed = "not_exposed"
	s3InternetExposureStateUnknown    = "unknown"

	s3InternetExposureReasonPublicPolicyAllowsPublic       = "public_policy_allows_public"
	s3InternetExposureReasonPublicPolicyRestrictedByBPA    = "public_policy_restricted_by_block_public_access"
	s3InternetExposureReasonPublicPolicyRestrictBPAUnknown = "public_policy_restrict_public_buckets_unknown"
	s3InternetExposureReasonPolicyPublicGrantUnknown       = "policy_public_grant_unknown"
	s3InternetExposureReasonNoPolicyACLPublicAccessBlocked = "no_policy_acl_public_access_blocked"
	s3InternetExposureReasonNoPublicPolicyGrant            = "no_public_policy_grant"
	s3InternetExposureReasonPartialPublicAccessBlock       = "partial_public_access_block"
	s3InternetExposureSkipSourceUnresolved                 = "source_unresolved"
)

type s3InternetExposureTally struct {
	decisions       map[string]int
	decisionReasons map[s3InternetExposureDecisionKey]int
	reasons         map[string]int
	skipped         map[string]int
}

func newS3InternetExposureTally() s3InternetExposureTally {
	return s3InternetExposureTally{
		decisions:       make(map[string]int),
		decisionReasons: make(map[s3InternetExposureDecisionKey]int),
		reasons:         make(map[string]int),
		skipped:         make(map[string]int),
	}
}

type s3InternetExposureDecisionKey struct {
	outcome string
	reason  string
}

func (t s3InternetExposureTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

type s3InternetExposureDecision struct {
	state           string
	internetExposed any
	reason          string
}

// ExtractS3InternetExposureRows derives reducer-owned S3 internet exposure
// node-property rows from s3_bucket_posture facts and the same scoped
// aws_resource S3 bucket join index used by LOGS_TO. It never fabricates bucket
// nodes: posture whose source bucket did not scan as an S3 CloudResource is
// counted as source_unresolved and produces no row. Unknown or partial posture
// produces state=unknown with a nil boolean exposure property, so absent
// evidence is never converted into a safe false.
func ExtractS3InternetExposureRows(
	resourceEnvelopes []facts.Envelope,
	postureEnvelopes []facts.Envelope,
) ([]map[string]any, s3InternetExposureTally, error) {
	tally := newS3InternetExposureTally()
	if len(postureEnvelopes) == 0 {
		return nil, tally, nil
	}

	index, err := buildS3BucketJoinIndex(resourceEnvelopes)
	if err != nil {
		return nil, tally, err
	}
	postures := sortedS3InternetExposurePostures(postureEnvelopes)
	seen := make(map[string]struct{}, len(postures))
	rows := make([]map[string]any, 0, len(postures))
	for _, env := range postures {
		sourceUID, ok := index.resolve(s3PostureBucketName(env))
		if !ok {
			tally.skipped[s3InternetExposureSkipSourceUnresolved]++
			continue
		}
		if _, duplicate := seen[sourceUID]; duplicate {
			continue
		}
		seen[sourceUID] = struct{}{}

		decision := deriveS3InternetExposureDecision(env.Payload)
		tally.decisions[decision.state]++
		tally.decisionReasons[s3InternetExposureDecisionKey{
			outcome: decision.state,
			reason:  decision.reason,
		}]++
		tally.reasons[decision.reason]++
		rows = append(rows, map[string]any{
			"uid":              sourceUID,
			"state":            decision.state,
			"internet_exposed": decision.internetExposed,
			"reason":           decision.reason,
			"source_fact_id":   env.FactID,
		})
	}

	if len(rows) == 0 {
		return nil, tally, nil
	}
	sort.Slice(rows, func(i, j int) bool {
		return anyToString(rows[i]["uid"]) < anyToString(rows[j]["uid"])
	})
	return rows, tally, nil
}

func sortedS3InternetExposurePostures(envelopes []facts.Envelope) []facts.Envelope {
	postures := make([]facts.Envelope, 0, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.S3BucketPostureFactKind {
			continue
		}
		postures = append(postures, env)
	}
	sort.SliceStable(postures, func(i, j int) bool {
		leftName := s3PostureBucketName(postures[i])
		rightName := s3PostureBucketName(postures[j])
		if leftName != rightName {
			return leftName < rightName
		}
		return postures[i].FactID < postures[j].FactID
	})
	return postures
}

func deriveS3InternetExposureDecision(payload map[string]any) s3InternetExposureDecision {
	policyPublic := payloadBoolPointer(payload, "policy_grants_public")
	if policyPublic != nil && *policyPublic {
		return deriveS3PublicPolicyDecision(payload)
	}
	if policyPublic == nil && payloadBool(payload, "policy_present") {
		return s3InternetExposureDecision{
			state:           s3InternetExposureStateUnknown,
			internetExposed: nil,
			reason:          s3InternetExposureReasonPolicyPublicGrantUnknown,
		}
	}
	if aclPublicAccessBlocked(payload) {
		reason := s3InternetExposureReasonNoPublicPolicyGrant
		if policyPresent := payloadBoolPointer(payload, "policy_present"); policyPresent != nil && !*policyPresent {
			reason = s3InternetExposureReasonNoPolicyACLPublicAccessBlocked
		}
		return s3InternetExposureDecision{
			state:           s3InternetExposureStateNotExposed,
			internetExposed: false,
			reason:          reason,
		}
	}
	return s3InternetExposureDecision{
		state:           s3InternetExposureStateUnknown,
		internetExposed: nil,
		reason:          s3InternetExposureReasonPartialPublicAccessBlock,
	}
}

func deriveS3PublicPolicyDecision(payload map[string]any) s3InternetExposureDecision {
	if allBPAEnabled(payload) {
		return s3InternetExposureDecision{
			state:           s3InternetExposureStateNotExposed,
			internetExposed: false,
			reason:          s3InternetExposureReasonPublicPolicyRestrictedByBPA,
		}
	}
	restrictPublicBuckets := payloadBoolPointer(payload, "restrict_public_buckets")
	if restrictPublicBuckets == nil {
		return s3InternetExposureDecision{
			state:           s3InternetExposureStateUnknown,
			internetExposed: nil,
			reason:          s3InternetExposureReasonPublicPolicyRestrictBPAUnknown,
		}
	}
	if *restrictPublicBuckets {
		return s3InternetExposureDecision{
			state:           s3InternetExposureStateNotExposed,
			internetExposed: false,
			reason:          s3InternetExposureReasonPublicPolicyRestrictedByBPA,
		}
	}
	return s3InternetExposureDecision{
		state:           s3InternetExposureStateExposed,
		internetExposed: true,
		reason:          s3InternetExposureReasonPublicPolicyAllowsPublic,
	}
}

func allBPAEnabled(payload map[string]any) bool {
	return payloadBool(payload, "block_public_access_all")
}

func aclPublicAccessBlocked(payload map[string]any) bool {
	return payloadBool(payload, "block_public_access_all") ||
		payloadBool(payload, "ignore_public_acls")
}
