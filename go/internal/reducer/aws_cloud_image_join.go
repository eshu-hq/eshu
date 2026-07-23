// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// lambdaFunctionUsesImageRelationshipType and
// ecsTaskDefinitionUsesImageRelationshipType are the two "uses image"
// aws_relationship verbs issue #5450 disposes of. They duplicate
// awscloud.RelationshipLambdaFunctionUsesImage /
// RelationshipECSTaskDefinitionUsesImage (go/internal/collector/awscloud/
// constants_lambda.go, constants_ecs.go) as literals rather than importing the
// collector package: the reducer package does not depend on collector
// packages (package-boundary rule), and every other AWS relationship_type
// value the reducer reads already flows through as an opaque fact string
// (aws_relationship_join.go's resolveTarget never imports awscloud either).
const (
	lambdaFunctionUsesImageRelationshipType    = "lambda_function_uses_image"
	ecsTaskDefinitionUsesImageRelationshipType = "ecs_task_definition_uses_image"
)

// awsCloudImageEdgeSkipReason names why an otherwise-matched "uses image"
// relationship fact did not produce a CloudResource -> ContainerImage edge.
// These are POLICY dispositions (a deliberate #5472 EXACT-ONLY decision), not
// resolution failures — the bounded, honest tally the completion log surfaces.
type awsCloudImageEdgeSkipReason string

const (
	// awsCloudImageSkipTagOnlyPostgresOnly marks ecs_task_definition_uses_image
	// relationships: a task DEFINITION's container image is tag-only (no
	// digest — ecs/relationships.go's taskDefinitionImageRelationships never
	// carries one), so per #5472 EXACT-ONLY policy it stays Postgres-only
	// rather than resolving through container_image_identity's derived
	// tag-resolution (see docs/internal/aws-relationship-edge-materialization-design.md
	// and the #5450 design decision recorded in this package's doc comment).
	awsCloudImageSkipTagOnlyPostgresOnly awsCloudImageEdgeSkipReason = "tag_only_postgres_only_policy"
	// awsCloudImageSkipNoDigest marks a lambda_function_uses_image relationship
	// whose resolved_image_uri attribute is empty (a non-container-image
	// Lambda package, or a container-image function AWS has not yet resolved a
	// digest for): no exact digest reference exists to promote.
	awsCloudImageSkipNoDigest awsCloudImageEdgeSkipReason = "no_resolved_digest"
	// awsCloudImageSkipUnparseableRef marks a resolved_image_uri that is
	// present but does not parse as a registry/repository@sha256:digest
	// reference: the value is not a fabricable ContainerImage identity.
	awsCloudImageSkipUnparseableRef awsCloudImageEdgeSkipReason = "unparseable_digest_ref"
	// awsCloudImageSkipSourceUnresolved marks a relationship whose source
	// CloudResource endpoint did not resolve in this scope generation's join
	// index.
	awsCloudImageSkipSourceUnresolved awsCloudImageEdgeSkipReason = "source_unresolved"
)

// awsCloudImageResolutionMode is the single resolution mode this domain's
// edge-projection counter and completion log use for a materialized edge: the
// two-MATCH-MERGE target is always an exact registry+repository@digest
// reference, never a derived or tag-resolved one.
const awsCloudImageResolutionMode = "container_image_digest"

// awsCloudImageEdgeTally is the bounded, honest accounting surface for the AWS
// cloud-image edge projection (mirrors awsRelationshipEdgeTally). resolved
// counts materialized edges; skipped counts every non-materializing "uses
// image" relationship fact keyed by its policy/resolution disposition, so an
// operator can distinguish "the tag-only ECS policy skip" from "the digest was
// unparseable" from "the source endpoint was not scanned" without a per-edge
// log line.
type awsCloudImageEdgeTally struct {
	resolved int
	skipped  map[awsCloudImageEdgeSkipReason]int
}

func newAWSCloudImageEdgeTally() awsCloudImageEdgeTally {
	return awsCloudImageEdgeTally{skipped: make(map[awsCloudImageEdgeSkipReason]int)}
}

func (t *awsCloudImageEdgeTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

// ExtractAWSCloudImageEdgeRows builds canonical CloudResource -> ContainerImage
// edge rows for the lambda_function_uses_image relationship (issue #5450).
// Unlike ExtractAWSRelationshipEdgeRows (CloudResource -> CloudResource), the
// target here is a :ContainerImage node whose uid is computed directly from
// the relationship's own resolved_image_uri attribute — never resolved
// against the aws_resource join index, because a container image is not an
// aws_resource. Both endpoints are still validated only through a two-MATCH
// (see the writer): an unscanned ContainerImage produces a no-op, never a
// fabricated node.
//
// ecs_task_definition_uses_image relationship facts are recognized but always
// skipped with awsCloudImageSkipTagOnlyPostgresOnly (the #5472 EXACT-ONLY
// policy decision documented on that constant) — they never produce a row.
// Every other relationship_type is ignored (out of this domain's scope).
//
// Returned rows are deduplicated by (source_uid, target_uid) and sorted
// deterministically so the batched write is stable across retries and
// reprojections.
func ExtractAWSCloudImageEdgeRows(
	resourceEnvelopes []facts.Envelope,
	relationshipEnvelopes []facts.Envelope,
) ([]map[string]any, awsCloudImageEdgeTally, []quarantinedFact, error) {
	tally := newAWSCloudImageEdgeTally()
	if len(relationshipEnvelopes) == 0 {
		return nil, tally, nil, nil
	}

	index, quarantined, err := buildCloudResourceJoinIndex(resourceEnvelopes)
	if err != nil {
		return nil, tally, nil, err
	}

	type edgeKey struct {
		source string
		target string
	}
	seen := make(map[edgeKey]struct{}, len(relationshipEnvelopes))
	rows := make([]map[string]any, 0, len(relationshipEnvelopes))

	for _, env := range relationshipEnvelopes {
		if env.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		relationship, err := decodeAWSRelationship(env)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, tally, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}

		switch relationship.RelationshipType {
		case ecsTaskDefinitionUsesImageRelationshipType:
			tally.skipped[awsCloudImageSkipTagOnlyPostgresOnly]++
			continue
		case lambdaFunctionUsesImageRelationshipType:
			// handled below
		default:
			continue
		}

		sourceARN := derefString(relationship.SourceARN)
		sourceResourceID := relationship.SourceResourceID
		sourceUID, sourceOK := index.resolveSource(sourceARN, sourceResourceID)
		if !sourceOK {
			tally.skipped[awsCloudImageSkipSourceUnresolved]++
			continue
		}

		imageAttrs, err := awsv1.DecodeRelationshipLambdaFunctionUsesImageAttributes(relationship)
		if err != nil {
			quarantined = append(quarantined, quarantinedAttributeShapeFact(env, err))
			continue
		}
		if imageAttrs.ResolvedImageURI == "" {
			tally.skipped[awsCloudImageSkipNoDigest]++
			continue
		}

		targetUID, ok := containerImageNodeUIDFromDigestRef(imageAttrs.ResolvedImageURI)
		if !ok {
			tally.skipped[awsCloudImageSkipUnparseableRef]++
			continue
		}

		key := edgeKey{source: sourceUID, target: targetUID}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		tally.resolved++
		rows = append(rows, map[string]any{
			"source_uid":        sourceUID,
			"target_uid":        targetUID,
			"relationship_type": relationship.RelationshipType,
			"resolution_mode":   awsCloudImageResolutionMode,
		})
	}

	if len(rows) == 0 {
		return nil, tally, quarantined, nil
	}

	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["source_uid"]) + "->" + anyToString(rows[a]["target_uid"])
		right := anyToString(rows[b]["source_uid"]) + "->" + anyToString(rows[b]["target_uid"])
		return left < right
	})
	return rows, tally, quarantined, nil
}

// containerImageNodeUIDFromDigestRef computes the exact :ContainerImage node
// uid a registry+repository@sha256:digest reference resolves to, matching the
// OCI registry canonical writer's own uid formula
// (internal/projector.ociDescriptorUID / ociResolvedDescriptorUID): given a
// repository_id of the form "oci-registry://<registry>/<repository>", the
// node uid is "oci-descriptor://<registry>/<repository>@<digest>". This
// function does NOT re-derive that formula through the projector package (the
// reducer package does not import projector); it independently computes the
// identical literal string from the two inputs both formulas share: the
// registry/repository portion before "@" and the digest after it.
//
// The registry/repository portion AND the digest are lowercased to match the
// OCI registry collector's own normalization
// (internal/collector/ociregistry/identity.go NormalizeRepositoryIdentity /
// normalizeDigest): the collector unconditionally lowercases the scanned
// registry, repository, and digest before computing repository_id/descriptor
// identity, so the REAL :ContainerImage node uid is always lowercase
// regardless of the case the source (an ECR API response, a Docker Hub
// manifest, etc.) reported. resolved_image_uri, by contrast, is read directly
// from the Lambda GetFunction API response and never passes through that
// collector normalization, so it can carry any case the registry/repository
// name happens to have. Without lowercasing here, a registry or repository
// name containing an uppercase character (ECR host/repo names are
// conventionally lowercase, but this is a documented AWS convention, not a
// hard API guarantee this reducer should silently depend on) would compute a
// uid that can never MATCH the real node the OCI registry collector creates —
// exactly the silent no-op #5450 exists to close. Lowercasing unconditionally
// (rather than trusting "ECR is conventionally lowercase") removes that
// otherwise-hidden dependency for the ECR-hosted references this domain
// actually processes; see the note below on why that is the reachable case,
// not a claim that this matches the collector for an arbitrary registry
// shape.
//
// This is proven equivalent for ECR-hosted references specifically — the only
// shape this domain ever processes, since AWS Lambda container images are
// exclusively ECR-hosted (a platform restriction, not a convention) — not for
// an arbitrary registry value. normalizeRegistry lowercases only the leading
// host segment when its input contains an embedded path (its url.Parse and
// Cut branches both split host from path and lowercase only the host,
// preserving the path's case); that branch is unreachable here because a bare
// ECR hostname (e.g. "123456789012.dkr.ecr.us-east-1.amazonaws.com") never
// contains a "/", so normalizeRegistry always takes its whole-string
// lowercase path for every reference this function actually sees.
//
// Returns ok=false for anything that is not a digest-qualified reference
// (no "@sha256:" suffix) or has an empty registry/repository portion — never
// a fabricated or partial uid.
func containerImageNodeUIDFromDigestRef(ref string) (string, bool) {
	trimmed := strings.TrimSpace(ref)
	before, rawDigest, ok := strings.Cut(trimmed, "@")
	if !ok {
		return "", false
	}
	// Lowercase first, matching the collector's own normalizeDigest order
	// (lowercase, then validate/use), so a mixed-case "SHA256:" prefix or hex
	// body normalizes identically to how the OCI registry collector would
	// have recorded the same digest.
	digest := strings.ToLower(strings.TrimSpace(rawDigest))
	if !strings.HasPrefix(digest, "sha256:") || len(digest) <= len("sha256:") {
		return "", false
	}
	repositoryKey := strings.ToLower(strings.Trim(strings.TrimSpace(before), "/"))
	if repositoryKey == "" {
		return "", false
	}
	return "oci-descriptor://" + repositoryKey + "@" + digest, true
}
