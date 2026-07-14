// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

const (
	s3ExternalPrincipalGrantRelationshipType = string(edgetype.GrantsAccessTo)
	s3ExternalPrincipalGrantResolutionMode   = "bucket_name"

	s3ExternalPrincipalKindPublic      = "public"
	s3ExternalPrincipalKindAWSAccount  = "aws_account"
	s3ExternalPrincipalKindAWSARN      = "aws_arn"
	s3ExternalPrincipalKindAWSService  = "aws_service"
	s3ExternalPrincipalKindUnsupported = "unsupported"

	s3ExternalPrincipalGrantSkipSourceUnresolved     = "source_unresolved"
	s3ExternalPrincipalGrantSkipUnsupportedPrincipal = "unsupported_principal"
	s3ExternalPrincipalGrantSkipMissingIdentity      = "missing_identity"
)

type s3ExternalPrincipalGrantTally struct {
	resolved map[string]int
	skipped  map[string]int
}

func newS3ExternalPrincipalGrantTally() s3ExternalPrincipalGrantTally {
	return s3ExternalPrincipalGrantTally{
		resolved: make(map[string]int),
		skipped:  make(map[string]int),
	}
}

func (t s3ExternalPrincipalGrantTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

// externalPrincipalUID computes the stable ExternalPrincipal node identity. The
// key intentionally uses only the exact principal kind and value; optional
// account, partition, and service metadata stay mutable properties so missing
// secondary metadata cannot split the same principal across nodes.
func externalPrincipalUID(principalKind, principalValue string) string {
	return facts.StableID("ExternalPrincipal", map[string]any{
		"principal_kind":  strings.TrimSpace(principalKind),
		"principal_value": strings.TrimSpace(principalValue),
	})
}

// ExtractS3ExternalPrincipalGrantRows builds canonical GRANTS_ACCESS_TO rows
// from one scope generation's aws_resource S3 bucket facts and metadata-only
// s3_external_principal_grant facts. It never propagates raw policy, statement,
// ACL, condition, or object fields: only bounded principal identity metadata and
// derived outcome booleans become row properties.
func ExtractS3ExternalPrincipalGrantRows(
	resourceEnvelopes []facts.Envelope,
	grantEnvelopes []facts.Envelope,
) ([]map[string]any, s3ExternalPrincipalGrantTally, []quarantinedFact, error) {
	tally := newS3ExternalPrincipalGrantTally()
	if len(grantEnvelopes) == 0 {
		return nil, tally, nil, nil
	}

	index, quarantined, err := buildS3BucketJoinIndex(resourceEnvelopes)
	if err != nil {
		return nil, tally, nil, err
	}
	type edgeKey struct {
		source    string
		principal string
	}
	seen := make(map[edgeKey]struct{}, len(grantEnvelopes))
	rows := make([]map[string]any, 0, len(grantEnvelopes))

	for _, env := range grantEnvelopes {
		if env.FactKind != facts.S3ExternalPrincipalGrantFactKind {
			continue
		}

		grant, err := decodeS3ExternalPrincipalGrant(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, tally, quarantined, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}

		sourceUID, ok := index.resolve(s3ExternalPrincipalGrantBucketName(grant))
		if !ok {
			tally.skipped[s3ExternalPrincipalGrantSkipSourceUnresolved]++
			continue
		}

		principalKind := strings.TrimSpace(grant.PrincipalKind)
		principalValue := strings.TrimSpace(grant.PrincipalValue)
		if principalKind == "" || principalValue == "" {
			tally.skipped[s3ExternalPrincipalGrantSkipMissingIdentity]++
			continue
		}
		if !s3ExternalPrincipalGrantKindIsGraphProjectable(principalKind) {
			tally.skipped[s3ExternalPrincipalGrantSkipUnsupportedPrincipal]++
			continue
		}

		principalUID := externalPrincipalUID(principalKind, principalValue)
		key := edgeKey{source: sourceUID, principal: principalUID}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		outcome := strings.TrimSpace(grant.GrantOutcome)
		tally.resolved[outcome]++
		rows = append(rows, map[string]any{
			"source_uid":           sourceUID,
			"principal_uid":        principalUID,
			"principal_kind":       principalKind,
			"principal_value":      principalValue,
			"principal_account_id": strings.TrimSpace(derefString(grant.PrincipalAccountID)),
			"principal_partition":  strings.TrimSpace(derefString(grant.PrincipalPartition)),
			"principal_service":    strings.TrimSpace(derefString(grant.PrincipalService)),
			"relationship_type":    s3ExternalPrincipalGrantRelationshipType,
			"grant_outcome":        outcome,
			"is_public":            grant.IsPublic,
			"is_cross_account":     grant.IsCrossAccount,
			"is_service_principal": grant.IsServicePrincipal,
			"resolution_mode":      s3ExternalPrincipalGrantResolutionMode,
		})
	}

	if len(rows) == 0 {
		return nil, tally, quarantined, nil
	}
	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["source_uid"]) + "->" + anyToString(rows[a]["principal_uid"])
		right := anyToString(rows[b]["source_uid"]) + "->" + anyToString(rows[b]["principal_uid"])
		return left < right
	})
	return rows, tally, quarantined, nil
}

func s3ExternalPrincipalGrantKindIsGraphProjectable(kind string) bool {
	switch strings.TrimSpace(kind) {
	case s3ExternalPrincipalKindPublic,
		s3ExternalPrincipalKindAWSAccount,
		s3ExternalPrincipalKindAWSARN,
		s3ExternalPrincipalKindAWSService:
		return true
	default:
		return false
	}
}

// s3ExternalPrincipalGrantBucketName resolves the decoded grant's source
// bucket name, falling back to deriving it from the bucket ARN tail when the
// bare name was not observed (the collector's NewS3ExternalPrincipalGrantEnvelope
// requires bucket_arn OR bucket_name, so exactly one may be blank).
func s3ExternalPrincipalGrantBucketName(grant awsv1.S3ExternalPrincipalGrant) string {
	if name := strings.TrimSpace(derefString(grant.BucketName)); name != "" {
		return name
	}
	return s3BucketNameFromARN(derefString(grant.BucketARN))
}
