// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// The s3_logs_to family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.S3LogsToEdgeWriter through the WriteS3LogsToEdges port. The
// relationship type is LOGS_TO, the single member of the writer's closed
// vocabulary substituted into the canonical upsert template's one %s; the
// MERGE keys on the stable (source_uid, LOGS_TO, target_uid) triple. Read it
// off the template and the s3LogsToRelationshipType const, never by deriving
// from the port or family name — "S3_LOGS_TO" is statement metadata, not a
// graph relationship type, and a name-derived literal would match no executed
// statement.
//
// Facts are built as typed awsv1.Resource / awsv1.S3BucketPosture values
// and encoded through factschema.EncodeAWSResource /
// factschema.EncodeS3BucketPosture, never as hand-built maps (Contract
// System v1).
//
// Scope note: S3 bucket nodes materialize from aws_resource facts, so this
// Odù carries the seven scanned bucket node facts the name join resolves
// against, plus six posture facts. A ghost log-bucket name is never scanned;
// the idle bucket scans with no posture referencing it, proving scanning
// alone never gains an edge.

const (
	// S3LogsToFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:s3_logs_to coverage row would name to resolve
	// through it.
	S3LogsToFamilyOduName = "odu:ifa-s3-logs-to-family"

	// s3LogsToFamilyScopeID is the single AWS account scope every fact in
	// this Odù belongs to. The reducer handler loads one scope
	// generation's facts, so a fixture spanning scopes would not mirror any
	// real intent.
	//
	// The scope is deliberately NOT any sibling cassette's scope.
	// Scopes carry one ACTIVE generation; driving a second generation into
	// an occupied scope supersedes the first family's generation, its
	// handler never runs, and the sibling exact-set assert fails with zero
	// edges — diagnosed live 2026-09-27 on the shared scope during the
	// iam_can_assume drive. One live generation per scope per cell is the
	// contract.
	s3LogsToFamilyScopeID = "aws:eshu-fixture-s3-logs-to-account"

	// s3LogsToFamilyAccountID is the account every fact below shares.
	// Synthetic, matching the collector's own redaction posture.
	s3LogsToFamilyAccountID = "123456789012"

	// s3LogsToFamilyRegion is the region every fact below carries. The aws
	// collector stamps us-east-1 on S3 facts; the join index folds region
	// into the node uid, so nodes and postures agree by construction.
	s3LogsToFamilyRegion = "us-east-1"

	// s3LogsToFamilyGenerationID is the one scope generation the Odù
	// replays. The reducer's handler loads a single scope generation's
	// facts, so every fact below shares it.
	s3LogsToFamilyGenerationID = "gen-ifa-s3-logs-to-family-1"

	// s3LogsToFamilyCollectorKind mirrors what the awscloud collector
	// stamps on both fact kinds, so the Odù describes the same envelopes a
	// live generation would carry rather than agreeing only on the payload.
	s3LogsToFamilyCollectorKind = "aws"

	// s3LogsToFamilySourceConfidence marks these facts as directly
	// observed, the posture a scanner-emitted resource carries.
	s3LogsToFamilySourceConfidence = "observed"
)

// The fixture bucket names. orders-bucket logs to logs-bucket (the edge);
// audit-bucket logs to itself (a legal S3 configuration that DOES emit an
// edge — the deliberate divergence from the IAM self-assume skip rule);
// plain-bucket has logging disabled; app-bucket names ghost-log-bucket,
// which the scope never scanned (target_unresolved, the conservative skip);
// orphan-bucket's posture arrives with no scanned node (source_unresolved);
// arnonly-bucket carries only the ARN, proving the ARN-tail name fallback;
// idle-bucket scans with no posture referencing it, proving a scanned node
// alone never gains an edge.
const (
	s3LogsToFamilyOrdersBucket  = "orders-bucket"
	s3LogsToFamilyLogsBucket    = "logs-bucket"
	s3LogsToFamilyAuditBucket   = "audit-bucket"
	s3LogsToFamilyPlainBucket   = "plain-bucket"
	s3LogsToFamilyAppBucket     = "app-bucket"
	s3LogsToFamilyGhostBucket   = "ghost-log-bucket"
	s3LogsToFamilyOrphanBucket  = "orphan-bucket"
	s3LogsToFamilyARNOnlyBucket = "arnonly-bucket"
	s3LogsToFamilyIdleBucket    = "idle-bucket"
)

// s3LogsToFamilyBucketARN returns the S3 ARN for a fixture bucket name, the
// collector's resource_id convention (firstNonEmpty(arn, name)).
func s3LogsToFamilyBucketARN(name string) string {
	return "arn:aws:s3:::" + name
}

// s3LogsToFamilyBucketFixture describes one aws_resource fact in the Odù:
// a scanned S3 bucket node the LOGS_TO name join resolves bucket names
// against. resource_id equals the ARN, the scanner convention the join
// index keys the node uid on.
type s3LogsToFamilyBucketFixture struct {
	// BucketName is the bare bucket name: the Name field, the ARN tail,
	// and the logging_target_bucket join key.
	BucketName string
}

// s3LogsToFamilyBuckets are the scanned node substrate: five edge
// endpoints, one scanned bucket with no posture that must never gain an
// edge on the strength of being scanned, and the ARN-only bucket's node.
// ghost-log-bucket and orphan-bucket are deliberately absent: the first is
// never scanned, the second's posture arrives with no node.
var s3LogsToFamilyBuckets = []s3LogsToFamilyBucketFixture{
	{BucketName: s3LogsToFamilyOrdersBucket},
	{BucketName: s3LogsToFamilyLogsBucket},
	{BucketName: s3LogsToFamilyAuditBucket},
	{BucketName: s3LogsToFamilyPlainBucket},
	{BucketName: s3LogsToFamilyAppBucket},
	{BucketName: s3LogsToFamilyARNOnlyBucket},
	{BucketName: s3LogsToFamilyIdleBucket},
}

// s3LogsToFamilyPostureFixture describes one s3_bucket_posture fact in the
// Odù.
type s3LogsToFamilyPostureFixture struct {
	// BucketName is the bare bucket name. Empty only where the source is
	// keyed by ARN: the reducer derives the name from the ARN tail.
	BucketName string
	// BucketARN is the bucket ARN. Always set: the emitter requires
	// bucket_arn OR bucket_name.
	BucketARN string
	// LoggingTargetBucket is the bare name of the bucket receiving this
	// bucket's access logs. Blank means logging is disabled: the normal
	// no-edge state, not a skip.
	LoggingTargetBucket string
}

// s3LogsToFamilyPostures are the buckets: two edge producers (one by name,
// one self-target), one ARN-only producer proving the ARN-tail fallback,
// one bucket with logging disabled, one naming an unscanned log bucket
// (the conservative skip), and one orphan posture with no scanned node.
//
// The non-producers are the load-bearing half. The extractor never
// fabricates an endpoint — a blank target, an unscanned target name, and
// an unscanned source each resolve to nothing — and the writer's two MATCH
// clauses would no-op on a missing node anyway. Without them, a regression
// that started emitting an edge for every posture regardless of target
// resolution would still reproduce the expected set exactly and this
// fixture would report green.
var s3LogsToFamilyPostures = []s3LogsToFamilyPostureFixture{
	{
		// EDGE: orders-bucket resolves the source by name, logs-bucket
		// resolves the target (name resolution mode).
		BucketName:          s3LogsToFamilyOrdersBucket,
		BucketARN:           s3LogsToFamilyBucketARN(s3LogsToFamilyOrdersBucket),
		LoggingTargetBucket: s3LogsToFamilyLogsBucket,
	},
	{
		// EDGE: self-target is a legal S3 configuration and DOES emit an
		// edge.
		BucketName:          s3LogsToFamilyAuditBucket,
		BucketARN:           s3LogsToFamilyBucketARN(s3LogsToFamilyAuditBucket),
		LoggingTargetBucket: s3LogsToFamilyAuditBucket,
	},
	{
		// EDGE: only the ARN observed — the source name derives from the
		// ARN tail and joins logs-bucket.
		BucketName:          "",
		BucketARN:           s3LogsToFamilyBucketARN(s3LogsToFamilyARNOnlyBucket),
		LoggingTargetBucket: s3LogsToFamilyLogsBucket,
	},
	{
		// NO EDGE: blank target — logging disabled, not a lost edge, and
		// not a skip.
		BucketName:          s3LogsToFamilyPlainBucket,
		BucketARN:           s3LogsToFamilyBucketARN(s3LogsToFamilyPlainBucket),
		LoggingTargetBucket: "",
	},
	{
		// NO EDGE: ghost log bucket never scanned — target_unresolved,
		// the conservative skip.
		BucketName:          s3LogsToFamilyAppBucket,
		BucketARN:           s3LogsToFamilyBucketARN(s3LogsToFamilyAppBucket),
		LoggingTargetBucket: s3LogsToFamilyGhostBucket,
	},
	{
		// NO EDGE: orphan posture with no scanned node — source_unresolved,
		// the conservative skip.
		BucketName:          s3LogsToFamilyOrphanBucket,
		BucketARN:           s3LogsToFamilyBucketARN(s3LogsToFamilyOrphanBucket),
		LoggingTargetBucket: s3LogsToFamilyLogsBucket,
	},
}

// s3LogsToFamilyStableFactKey derives one fact's durable dedup key from the
// same identity inputs the collector keys its StableID by, in a readable
// fixture-local format rather than a hex digest, instead of hand-typing a
// string the expected-edge fixture cannot independently check.
//
// The posture half MUST include the bucket identity, not just the log
// target: several fixture postures share targets and differ only in source
// (edge producers vs disabled vs ghost vs orphan), so keying on target
// alone would collapse distinct postures: live ingest would drive fewer
// facts than claimed and negative controls would never reach the
// extractor. Key narrowness is a silent live-coverage loss the offline
// guard cannot see (it runs over all compiled facts), so the derivation
// below carries every collector identity input the fixture varies — the
// ARN where the bucket name is blank.
func s3LogsToFamilyStableFactKey(factKind, identity string) string {
	return fmt.Sprintf(
		"aws:%s:us-east-1:%s:%s",
		s3LogsToFamilyAccountID,
		factKind, identity,
	)
}

// s3LogsToFamilyPostureIdentity returns the identity input the stable key
// carries for one posture: the bucket name, or the ARN where the name is
// blank.
func s3LogsToFamilyPostureIdentity(fixture s3LogsToFamilyPostureFixture) string {
	if fixture.BucketName != "" {
		return fixture.BucketName
	}
	return fixture.BucketARN
}

// S3LogsToFamilyOdu builds the cataloged Odù for the s3_logs_to
// direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason EC2UsesProfileFamilyOdu does: a failure means
// the payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func S3LogsToFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(s3LogsToFamilyBuckets)+len(s3LogsToFamilyPostures))
	for _, fixture := range s3LogsToFamilyBuckets {
		arn := s3LogsToFamilyBucketARN(fixture.BucketName)
		name := fixture.BucketName
		resource := awsv1.Resource{
			AccountID:    s3LogsToFamilyAccountID,
			ResourceID:   arn,
			Region:       s3LogsToFamilyRegion,
			ResourceType: awsv1.ResourceTypeS3Bucket,
			ARN:          &arn,
			Name:         &name,
		}
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_resource payload for %q: %v",
				S3LogsToFamilyOduName, fixture.BucketName, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          s3LogsToFamilyScopeID,
			GenerationID:     s3LogsToFamilyGenerationID,
			FactKind:         facts.AWSResourceFactKind,
			StableFactKey:    s3LogsToFamilyStableFactKey(facts.AWSResourceFactKind, fixture.BucketName),
			SchemaVersion:    facts.AWSResourceSchemaVersion,
			CollectorKind:    s3LogsToFamilyCollectorKind,
			SourceConfidence: s3LogsToFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for _, fixture := range s3LogsToFamilyPostures {
		bucketName := fixture.BucketName
		bucketARN := fixture.BucketARN
		target := fixture.LoggingTargetBucket
		posture := awsv1.S3BucketPosture{
			AccountID:           s3LogsToFamilyAccountID,
			Region:              s3LogsToFamilyRegion,
			BucketARN:           &bucketARN,
			BucketName:          &bucketName,
			LoggingTargetBucket: &target,
		}
		payload, err := factschema.EncodeS3BucketPosture(posture)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode s3_bucket_posture payload for %q: %v",
				S3LogsToFamilyOduName, s3LogsToFamilyPostureIdentity(fixture), err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          s3LogsToFamilyScopeID,
			GenerationID:     s3LogsToFamilyGenerationID,
			FactKind:         facts.S3BucketPostureFactKind,
			StableFactKey:    s3LogsToFamilyStableFactKey(facts.S3BucketPostureFactKind, s3LogsToFamilyPostureIdentity(fixture)),
			SchemaVersion:    facts.S3BucketPostureSchemaVersionV1,
			CollectorKind:    s3LogsToFamilyCollectorKind,
			SourceConfidence: s3LogsToFamilySourceConfidence,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: S3LogsToFamilyOduName, Facts: factsForOdu},
		Detail: "thirteen facts for the direct-materialization s3_logs_to family: seven aws_resource S3 bucket node facts (five edge endpoints plus one scanned bucket with no posture and the ARN-only bucket's node) and six s3_bucket_posture facts (one edge producer by name, one self-target resolved by name, one ARN-keyed producer proving the ARN-tail fallback, one bucket with logging disabled, one naming an unscanned log bucket, and one orphan posture with no scanned node), so the LOGS_TO expected set proves the name resolution mode and restraint",
	}
}
