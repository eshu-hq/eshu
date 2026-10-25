// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu-sdk/factschema"
	awsv1 "github.com/eshu-hq/eshu-sdk/factschema/aws/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

// The s3_external_principal_grant family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.S3ExternalPrincipalGrantWriter through the
// WriteS3ExternalPrincipalGrants port. The relationship type is
// GRANTS_ACCESS_TO, the single member of the writer's closed vocabulary
// substituted into the canonical upsert template's one %s. Read it off the
// template and the vocabulary, never by deriving from the port or family
// name.
//
// Facts are built as typed awsv1.Resource / awsv1.S3ExternalPrincipalGrant
// values and encoded through factschema.EncodeAWSResource /
// factschema.EncodeS3ExternalPrincipalGrant, never as hand-built maps
// (Contract System v1). The typed grant struct carries no policy, statement,
// ACL, condition, or object fields at all, so the extractor's redaction
// boundary (only bounded principal identity metadata and derived outcome
// booleans become row properties) holds by construction here.
//
// Scope note: bucket nodes materialize from aws_resource facts, so this Odù
// carries the four scanned bucket node facts the bucket-name join resolves
// against, plus seven grant facts. A bucket name with no scanned fact never
// resolves; an idle bucket with no grant referencing it proves scanning
// alone never gains an edge.

const (
	// S3ExternalPrincipalGrantFamilyOduName is this Odù's catalog name, the
	// ref a materialized_edges:s3_external_principal_grant coverage row
	// would name to resolve through it.
	S3ExternalPrincipalGrantFamilyOduName = "odu:ifa-s3-external-principal-grant-family"

	// s3ExternalPrincipalGrantFamilyScopeID is the single AWS account scope
	// every fact in this Odù belongs to. The reducer handler loads one scope
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
	s3ExternalPrincipalGrantFamilyScopeID = "aws:eshu-fixture-s3-external-principal-grant-account"

	// s3ExternalPrincipalGrantFamilyAccountID is the account every fact
	// below shares. Synthetic, matching the collector's own redaction
	// posture.
	s3ExternalPrincipalGrantFamilyAccountID = "123456789012"

	// s3ExternalPrincipalGrantFamilyRegion is the region every fact below
	// carries. The aws collector stamps us-east-1 on S3 facts; the join
	// index folds region into the node uid, so nodes and grants agree by
	// construction.
	s3ExternalPrincipalGrantFamilyRegion = "us-east-1"

	// s3ExternalPrincipalGrantFamilyGenerationID is the one scope generation
	// the Odù replays. The reducer's handler loads a single scope
	// generation's facts, so every fact below shares it.
	s3ExternalPrincipalGrantFamilyGenerationID = "gen-ifa-s3-external-principal-grant-family-1"

	// s3ExternalPrincipalGrantFamilyCollectorKind mirrors what the awscloud
	// collector stamps on both fact kinds, so the Odù describes the same
	// envelopes a live generation would carry rather than agreeing only on
	// the payload.
	s3ExternalPrincipalGrantFamilyCollectorKind = "aws"

	// s3ExternalPrincipalGrantFamilySourceConfidence marks these facts as
	// directly observed, the posture a scanner-emitted resource carries.
	s3ExternalPrincipalGrantFamilySourceConfidence = "observed"
)

// s3ExternalPrincipalGrantFamilyBucketARN returns the S3 ARN for a fixture
// bucket name, the collector's resource_id convention.
func s3ExternalPrincipalGrantFamilyBucketARN(name string) string {
	return "arn:aws:s3:::" + name
}

// The fixture bucket names. orders-artifacts, logs-archive, and
// shared-reads back edge-producing grants; idle-bucket scans with no grant
// referencing it, proving a scanned node alone never gains an edge;
// ghost-bucket is deliberately never scanned, so the grant naming it proves
// the source-unresolved skip.
const (
	s3ExternalPrincipalGrantFamilyOrdersBucket = "orders-artifacts"
	s3ExternalPrincipalGrantFamilyLogsBucket   = "logs-archive"
	s3ExternalPrincipalGrantFamilySharedBucket = "shared-reads"
	s3ExternalPrincipalGrantFamilyIdleBucket   = "idle-bucket"
	s3ExternalPrincipalGrantFamilyGhostBucket  = "ghost-bucket"
)

// s3ExternalPrincipalGrantFamilyBuckets are the scanned node substrate. The
// grant join keys each bucket by name (or ARN tail), so resource_id equals
// the ARN and Name carries the bare name, the scanner convention.
var s3ExternalPrincipalGrantFamilyBuckets = []string{
	s3ExternalPrincipalGrantFamilyOrdersBucket,
	s3ExternalPrincipalGrantFamilyLogsBucket,
	s3ExternalPrincipalGrantFamilySharedBucket,
	s3ExternalPrincipalGrantFamilyIdleBucket,
}

// s3ExternalPrincipalGrantFamilyGrantFixture describes one
// s3_external_principal_grant fact in the Odù.
type s3ExternalPrincipalGrantFamilyGrantFixture struct {
	// BucketName is the bare bucket name. Empty only where the source is
	// keyed by ARN: the reducer derives the name from the ARN tail.
	BucketName string
	// PrincipalKind is the bounded principal discriminator (public,
	// aws_account, aws_arn, aws_service, or unsupported).
	PrincipalKind string
	// PrincipalValue is the principal identity for its kind.
	PrincipalValue string
	// PrincipalAccountID is the owning account for account-kind
	// principals. Nil elsewhere.
	PrincipalAccountID *string
	// PrincipalService is the service identity for service-kind
	// principals. Nil elsewhere.
	PrincipalService *string
	// GrantOutcome is the derived outcome string the tally counts.
	GrantOutcome string
	// IsPublic, IsCrossAccount, IsServicePrincipal, and IsUnsupported are
	// the derived outcome booleans the writer stamps onto the edge.
	IsPublic           bool
	IsCrossAccount     bool
	IsServicePrincipal bool
	IsUnsupported      bool
}

// strPtr boxes an optional fixture string.
func strPtr(value string) *string {
	return &value
}

// s3ExternalPrincipalGrantFamilyGrants are the grants: four edge producers
// (one per projectable principal kind, one proving the ARN-tail source
// fallback) and three deliberate non-producers covering the
// source-unresolved, unsupported-principal, and missing-identity skips.
//
// The non-producers are the load-bearing half. The extractor never
// fabricates an endpoint — an unscanned source bucket, an unprojectable
// principal kind, and a grant with no principal identity each resolve to
// nothing — and the writer's source MATCH would no-op on a missing bucket
// anyway. Without them, a regression that started emitting an edge for every
// grant regardless of resolution would still reproduce the expected set
// exactly and this fixture would report green.
var s3ExternalPrincipalGrantFamilyGrants = []s3ExternalPrincipalGrantFamilyGrantFixture{
	{
		// EDGE: cross-account grant on a name-keyed bucket.
		BucketName:         s3ExternalPrincipalGrantFamilyOrdersBucket,
		PrincipalKind:      "aws_account",
		PrincipalValue:     "222233334444",
		PrincipalAccountID: strPtr("222233334444"),
		GrantOutcome:       "cross_account",
		IsCrossAccount:     true,
	},
	{
		// EDGE: public grant. The whole world is the principal; the edge
		// still keys on the bounded (kind, value) identity.
		BucketName:     s3ExternalPrincipalGrantFamilyLogsBucket,
		PrincipalKind:  "public",
		PrincipalValue: "*",
		GrantOutcome:   "public",
		IsPublic:       true,
	},
	{
		// EDGE: service-principal grant.
		BucketName:         s3ExternalPrincipalGrantFamilySharedBucket,
		PrincipalKind:      "aws_service",
		PrincipalValue:     "logging.s3.amazonaws.com",
		PrincipalService:   strPtr("logging.s3.amazonaws.com"),
		GrantOutcome:       "service_delivery",
		IsServicePrincipal: true,
	},
	{
		// EDGE: only the ARN observed — the source name derives from the
		// ARN tail and joins shared-reads.
		BucketName:         "",
		PrincipalKind:      "aws_account",
		PrincipalValue:     "333344445555",
		PrincipalAccountID: strPtr("333344445555"),
		GrantOutcome:       "cross_account",
		IsCrossAccount:     true,
	},
	{
		// NO EDGE: ghost bucket never scanned — source_unresolved, the
		// conservative skip.
		BucketName:     s3ExternalPrincipalGrantFamilyGhostBucket,
		PrincipalKind:  "aws_account",
		PrincipalValue: "444455556666",
		GrantOutcome:   "cross_account",
		IsCrossAccount: true,
	},
	{
		// NO EDGE: unprojectable principal kind — unsupported_principal,
		// counted and never fabricated.
		BucketName:     s3ExternalPrincipalGrantFamilyOrdersBucket,
		PrincipalKind:  "unsupported",
		PrincipalValue: "federated-oidc-provider",
		GrantOutcome:   "unsupported_principal",
		IsUnsupported:  true,
	},
	{
		// NO EDGE: grant with no principal identity — missing_identity.
		// Blank kind and value still decode (the schema requires the keys,
		// not their contents), so this reaches the extractor's skip rather
		// than the quarantine path.
		BucketName:     s3ExternalPrincipalGrantFamilyLogsBucket,
		PrincipalKind:  "",
		PrincipalValue: "",
		GrantOutcome:   "missing_identity",
	},
}

// s3ExternalPrincipalGrantFamilyStableFactKey derives one fact's durable
// dedup key from the identity the collector would key it by, rather than
// hand-typing a string the expected-edge fixture cannot independently check.
//
// The grant half MUST carry the bucket plus the principal identity, not
// just the bucket: several fixture grants share buckets and differ only in
// principal (edge producers vs unsupported vs missing-identity), so keying
// on bucket alone would collapse distinct grants: live ingest would drive
// fewer facts than claimed and negative controls would never reach the
// extractor. Key narrowness is invisible to every other check in this family
// (the offline guard runs over the compiled facts, never through ingest
// dedup), so it is pinned here and by the uniqueness test below rather than
// assumed from the derivation shape.
func s3ExternalPrincipalGrantFamilyStableFactKey(factKind, identity string) string {
	return fmt.Sprintf(
		"aws:%s:us-east-1:%s:%s",
		s3ExternalPrincipalGrantFamilyAccountID,
		factKind, identity,
	)
}

// s3ExternalPrincipalGrantFamilyGrantBucketARN resolves one grant fixture's
// source ARN: the named bucket's ARN, or shared-reads' ARN where the name is
// blank (the ARN-tail fallback case carries only the ARN, exactly as the
// collector emits it when bucket_name is unobserved).
func s3ExternalPrincipalGrantFamilyGrantBucketARN(fixture s3ExternalPrincipalGrantFamilyGrantFixture) string {
	if fixture.BucketName != "" {
		return s3ExternalPrincipalGrantFamilyBucketARN(fixture.BucketName)
	}
	return s3ExternalPrincipalGrantFamilyBucketARN(s3ExternalPrincipalGrantFamilySharedBucket)
}

// s3ExternalPrincipalGrantFamilyGrantIdentity returns the identity input the
// stable key carries for one grant: bucket plus principal kind and value,
// or the ARN where the name is blank.
func s3ExternalPrincipalGrantFamilyGrantIdentity(fixture s3ExternalPrincipalGrantFamilyGrantFixture) string {
	bucket := fixture.BucketName
	if bucket == "" {
		bucket = s3ExternalPrincipalGrantFamilyGrantBucketARN(fixture)
	}
	return fmt.Sprintf("%s:%s:%s", bucket, fixture.PrincipalKind, fixture.PrincipalValue)
}

// S3ExternalPrincipalGrantFamilyOdu builds the cataloged Odù for the
// s3_external_principal_grant direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason S3LogsToFamilyOdu does: a failure means the
// payload contract moved under a committed fixture, and every coverage claim
// built on it is already void.
func S3ExternalPrincipalGrantFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(s3ExternalPrincipalGrantFamilyBuckets)+len(s3ExternalPrincipalGrantFamilyGrants))
	for _, bucket := range s3ExternalPrincipalGrantFamilyBuckets {
		arn := s3ExternalPrincipalGrantFamilyBucketARN(bucket)
		name := bucket
		resource := awsv1.Resource{
			AccountID:    s3ExternalPrincipalGrantFamilyAccountID,
			ResourceID:   arn,
			Region:       s3ExternalPrincipalGrantFamilyRegion,
			ResourceType: awsv1.ResourceTypeS3Bucket,
			ARN:          &arn,
			Name:         &name,
		}
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_resource payload for %q: %v",
				S3ExternalPrincipalGrantFamilyOduName, bucket, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          s3ExternalPrincipalGrantFamilyScopeID,
			GenerationID:     s3ExternalPrincipalGrantFamilyGenerationID,
			FactKind:         cloud.AWSResourceFactKind,
			StableFactKey:    s3ExternalPrincipalGrantFamilyStableFactKey(cloud.AWSResourceFactKind, bucket),
			SchemaVersion:    cloud.AWSResourceSchemaVersion,
			CollectorKind:    s3ExternalPrincipalGrantFamilyCollectorKind,
			SourceConfidence: s3ExternalPrincipalGrantFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for _, fixture := range s3ExternalPrincipalGrantFamilyGrants {
		bucketARN := s3ExternalPrincipalGrantFamilyGrantBucketARN(fixture)
		bucketName := fixture.BucketName
		grant := awsv1.S3ExternalPrincipalGrant{
			AccountID:          s3ExternalPrincipalGrantFamilyAccountID,
			Region:             s3ExternalPrincipalGrantFamilyRegion,
			BucketARN:          &bucketARN,
			BucketName:         &bucketName,
			PrincipalKind:      fixture.PrincipalKind,
			PrincipalValue:     fixture.PrincipalValue,
			PrincipalAccountID: fixture.PrincipalAccountID,
			PrincipalPartition: strPtr("aws"),
			PrincipalService:   fixture.PrincipalService,
			GrantOutcome:       fixture.GrantOutcome,
			IsPublic:           fixture.IsPublic,
			IsCrossAccount:     fixture.IsCrossAccount,
			IsServicePrincipal: fixture.IsServicePrincipal,
			IsUnsupported:      fixture.IsUnsupported,
		}
		payload, err := factschema.EncodeS3ExternalPrincipalGrant(grant)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode s3_external_principal_grant payload for %q: %v",
				S3ExternalPrincipalGrantFamilyOduName, s3ExternalPrincipalGrantFamilyGrantIdentity(fixture), err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          s3ExternalPrincipalGrantFamilyScopeID,
			GenerationID:     s3ExternalPrincipalGrantFamilyGenerationID,
			FactKind:         cloud.S3ExternalPrincipalGrantFactKind,
			StableFactKey:    s3ExternalPrincipalGrantFamilyStableFactKey(cloud.S3ExternalPrincipalGrantFactKind, s3ExternalPrincipalGrantFamilyGrantIdentity(fixture)),
			SchemaVersion:    cloud.S3ExternalPrincipalGrantSchemaVersionV1,
			CollectorKind:    s3ExternalPrincipalGrantFamilyCollectorKind,
			SourceConfidence: s3ExternalPrincipalGrantFamilySourceConfidence,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: S3ExternalPrincipalGrantFamilyOduName, Facts: factsForOdu},
		Detail: "eleven facts for the direct-materialization s3_external_principal_grant family: four aws_resource S3 bucket node facts (three edge endpoints plus one scanned bucket with no grant) and seven s3_external_principal_grant facts (one cross-account grant by name, one public grant, one service-principal grant, one ARN-keyed cross-account grant proving the ARN-tail fallback, one naming an unscanned bucket, one with an unprojectable principal kind, and one with no principal identity), so the GRANTS_ACCESS_TO expected set proves the bucket join and restraint",
	}
}
