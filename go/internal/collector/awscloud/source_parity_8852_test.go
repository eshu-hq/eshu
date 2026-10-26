// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// parity8852Fixture is the decoded cutover comparison input in
// testdata/8852-parity-awscloud.json.
type parity8852Fixture struct {
	ScopeID             string `json:"scope_id"`
	GenerationID        string `json:"generation_id"`
	CollectorInstanceID string `json:"collector_instance_id"`
	Resource            struct {
		ARN               string            `json:"arn"`
		ResourceID        string            `json:"resource_id"`
		ResourceType      string            `json:"resource_type"`
		Name              string            `json:"name"`
		State             string            `json:"state"`
		Tags              map[string]string `json:"tags"`
		Attributes        map[string]any    `json:"attributes"`
		CorrelationAnchor []string          `json:"correlation_anchors"`
	} `json:"resource"`
	Relationship struct {
		RelationshipType string `json:"relationship_type"`
		SourceResourceID string `json:"source_resource_id"`
		SourceARN        string `json:"source_arn"`
		TargetResourceID string `json:"target_resource_id"`
		TargetARN        string `json:"target_arn"`
		TargetType       string `json:"target_type"`
	} `json:"relationship"`
	DNSRecord struct {
		HostedZoneID      string   `json:"hosted_zone_id"`
		HostedZoneName    string   `json:"hosted_zone_name"`
		HostedZonePrivate bool     `json:"hosted_zone_private"`
		RecordName        string   `json:"record_name"`
		RecordType        string   `json:"record_type"`
		TTL               *int64   `json:"ttl"`
		Values            []string `json:"values"`
		AliasTarget       struct {
			DNSName              string `json:"dns_name"`
			HostedZoneID         string `json:"hosted_zone_id"`
			EvaluateTargetHealth bool   `json:"evaluate_target_health"`
		} `json:"alias_target"`
		RoutingPolicy struct {
			HealthCheckID string `json:"health_check_id"`
		} `json:"routing_policy"`
	} `json:"dns_record"`
	S3Posture struct {
		BucketARN           string `json:"bucket_arn"`
		BucketName          string `json:"bucket_name"`
		VersioningStatus    string `json:"versioning_status"`
		VersioningEnabled   bool   `json:"versioning_enabled"`
		SSEKMSKeyARN        string `json:"sse_kms_key_arn"`
		LoggingTargetBucket string `json:"logging_target_bucket"`
	} `json:"s3_posture"`
	IAMPermission struct {
		PrincipalARN             string   `json:"principal_arn"`
		PrincipalType            string   `json:"principal_type"`
		PolicySource             string   `json:"policy_source"`
		PolicyName               string   `json:"policy_name"`
		StatementSID             string   `json:"statement_sid"`
		Effect                   string   `json:"effect"`
		Actions                  []string `json:"actions"`
		Resources                []string `json:"resources"`
		ConditionKeys            []string `json:"condition_keys"`
		ConditionOperators       []string `json:"condition_operators"`
		ConditionValuesForbidden []string `json:"condition_values_must_not_appear"`
	} `json:"iam_permission"`
	Warnings map[string]struct {
		WarningKind string `json:"warning_kind"`
		ErrorClass  string `json:"error_class"`
		Message     string `json:"message"`
	} `json:"warnings"`
	Redaction struct {
		SecretOutputKey      string `json:"secret_output_key"`
		SecretOutputValue    string `json:"secret_output_value"`
		PlainOutputKey       string `json:"plain_output_key"`
		PlainOutputValue     string `json:"plain_output_value"`
		StatusMessageSecrets string `json:"status_message_with_secrets"`
	} `json:"redaction"`
	Hostile struct {
		BlankARN          string `json:"blank_arn"`
		BlankResourceID   string `json:"blank_resource_id"`
		BlankRecordName   string `json:"blank_record_name"`
		BlankPrincipalARN string `json:"blank_principal_arn"`
	} `json:"hostile"`
}

// Frozen in-tree stable keys for the representative fixture at the prep
// commit. The extracted producer must emit byte-identical keys for these
// inputs; any derivation change breaks these assertions by design.
const (
	parity8852ResourceKey      = "6734b22d6e0d6d006d039191b646a44444ceffc8e176b312545ff739567a9b0f"
	parity8852RelationshipKey  = "717afc75bf67a97a7573481ef28a2281c400cfce58ae598709509eb3845499e5"
	parity8852DNSRecordKey     = "5bd39ab9188007203e5eed1bd57eac7bfcc6cfb8ee5a86b6cdea2a64e09983e6"
	parity8852S3PostureKey     = "97ae4a36aa7fd60adc765e7c66a0dbd17321d6f5f17a7c537010d860ac6f8815"
	parity8852IAMPermissionKey = "147e19d094eb81dd4587b18c1e86bb1358ab5d6d7f8367fd75982bf6aac291ab"
	parity8852AssumeRoleKey    = "39743cefcca27096878bf129ae0564471c18235d99a7dea6cafacb6a57319130"
	parity8852ThrottleKey      = "a3df8d0052681fe04983da69766f754c1adfa298b3c8f09185a95ae4aa75f84d"
	parity8852BudgetKey        = "cba4c52eb4526a0edff664b75cc829690051e355d010102e1c30cdb235522e6e"
)

func loadParity8852Fixture(t *testing.T) parity8852Fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "8852-parity-awscloud.json"))
	if err != nil {
		t.Fatalf("ReadFile(parity fixture) error = %v, want nil", err)
	}
	var fixture parity8852Fixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("Unmarshal(parity fixture) error = %v, want nil", err)
	}
	return fixture
}

func parity8852Boundary(fixture parity8852Fixture, serviceKind string) Boundary {
	return Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         serviceKind,
		ScopeID:             fixture.ScopeID,
		GenerationID:        fixture.GenerationID,
		CollectorInstanceID: fixture.CollectorInstanceID,
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 10, 20, 13, 0, 0, 0, time.UTC),
	}
}

// parity8852CanonicalJSON renders a payload deterministically
// (encoding/json sorts map keys), so the cutover gate can compare live vs
// extracted output bytes without tripping on Go map iteration order.
func parity8852CanonicalJSON(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

func parity8852RedactKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("parity-8852-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	return key
}

// TestParity8852ResourceContract pins the in-tree side of the #8852 cutover
// comparison for aws_resource.
func TestParity8852ResourceContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8852Fixture(t)
	boundary := parity8852Boundary(fixture, catalog.ServiceIAM)
	in := fixture.Resource
	env, err := NewResourceEnvelope(ResourceObservation{
		Boundary:           boundary,
		ARN:                in.ARN,
		ResourceID:         in.ResourceID,
		ResourceType:       in.ResourceType,
		Name:               in.Name,
		State:              in.State,
		Tags:               in.Tags,
		Attributes:         in.Attributes,
		CorrelationAnchors: in.CorrelationAnchor,
	})
	if err != nil {
		t.Fatalf("NewResourceEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, cloud.AWSResourceFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.FactKind, "aws_resource"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.ScopeID, fixture.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := env.GenerationID, fixture.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := env.SchemaVersion, cloud.AWSResourceSchemaVersion; got != want {
		t.Fatalf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := env.SourceConfidence, facts.SourceConfidenceReported; got != want {
		t.Fatalf("SourceConfidence = %q, want %q", got, want)
	}
	if got, want := env.StableFactKey, parity8852ResourceKey; got != want {
		t.Fatalf("StableFactKey = %q, want frozen %q", got, want)
	}
	if got, want := env.Payload["resource_type"], in.ResourceType; got != want {
		t.Fatalf("Payload[resource_type] = %#v, want %#v", got, want)
	}
	if got, want := env.Payload["arn"], in.ARN; got != want {
		t.Fatalf("Payload[arn] = %#v, want %#v", got, want)
	}
	raw := parity8852CanonicalJSON(t, env.Payload)
	for _, want := range []string{in.ARN, "parity", "/parity/"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("canonical payload bytes miss %q: %s", want, raw)
		}
	}

	// Stable identity survives generation rotation; FactID is
	// generation-scoped, so retries and duplicate delivery stay idempotent.
	rotated := boundary
	rotated.GenerationID = "aws:123456789012:us-east-1:parity:2"
	second, err := NewResourceEnvelope(ResourceObservation{
		Boundary:     rotated,
		ARN:          in.ARN,
		ResourceID:   in.ResourceID,
		ResourceType: in.ResourceType,
	})
	if err != nil {
		t.Fatalf("NewResourceEnvelope(rotated) error = %v, want nil", err)
	}
	if second.StableFactKey != env.StableFactKey {
		t.Fatalf("StableFactKey rotated across generations: %q vs %q", second.StableFactKey, env.StableFactKey)
	}
	if got, want := second.StableFactKey, parity8852ResourceKey; got != want {
		t.Fatalf("StableFactKey(rotated) = %q, want frozen %q", got, want)
	}
	if second.FactID == env.FactID {
		t.Fatal("FactID did not change across generations, want generation-scoped identity")
	}
}

// TestParity8852RelationshipContract pins the in-tree side for
// aws_relationship.
func TestParity8852RelationshipContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8852Fixture(t)
	boundary := parity8852Boundary(fixture, catalog.ServiceIAM)
	in := fixture.Relationship
	env, err := NewRelationshipEnvelope(RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: in.RelationshipType,
		SourceResourceID: in.SourceResourceID,
		SourceARN:        in.SourceARN,
		TargetResourceID: in.TargetResourceID,
		TargetARN:        in.TargetARN,
		TargetType:       in.TargetType,
	})
	if err != nil {
		t.Fatalf("NewRelationshipEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, cloud.AWSRelationshipFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.Payload["relationship_type"], in.RelationshipType; got != want {
		t.Fatalf("Payload[relationship_type] = %#v, want %#v", got, want)
	}
	if got, want := env.StableFactKey, parity8852RelationshipKey; got != want {
		t.Fatalf("StableFactKey = %q, want frozen %q", got, want)
	}
}

// TestParity8852DNSRecordContract pins the in-tree side for aws_dns_record,
// including record-type normalization and trailing-dot handling.
func TestParity8852DNSRecordContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8852Fixture(t)
	boundary := parity8852Boundary(fixture, catalog.ServiceRoute53)
	in := fixture.DNSRecord
	env, err := NewDNSRecordEnvelope(DNSRecordObservation{
		Boundary:          boundary,
		HostedZoneID:      in.HostedZoneID,
		HostedZoneName:    in.HostedZoneName,
		HostedZonePrivate: in.HostedZonePrivate,
		RecordName:        in.RecordName,
		RecordType:        in.RecordType,
		TTL:               in.TTL,
		Values:            in.Values,
		AliasTarget: &DNSAliasTarget{
			DNSName:              in.AliasTarget.DNSName,
			HostedZoneID:         in.AliasTarget.HostedZoneID,
			EvaluateTargetHealth: in.AliasTarget.EvaluateTargetHealth,
		},
		RoutingPolicy: DNSRoutingPolicy{
			HealthCheckID: in.RoutingPolicy.HealthCheckID,
		},
	})
	if err != nil {
		t.Fatalf("NewDNSRecordEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, cloud.AWSDNSRecordFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.SchemaVersion, cloud.AWSDNSRecordSchemaVersion; got != want {
		t.Fatalf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := env.Payload["record_type"], "A"; got != want {
		t.Fatalf("Payload[record_type] = %#v, want %#v (normalized)", got, want)
	}
	if got, want := env.Payload["normalized_record_name"], "api.parity.example.com"; got != want {
		t.Fatalf("Payload[normalized_record_name] = %#v, want %#v", got, want)
	}
	if got, want := env.StableFactKey, parity8852DNSRecordKey; got != want {
		t.Fatalf("StableFactKey = %q, want frozen %q", got, want)
	}
}

// TestParity8852S3PostureContract pins the in-tree side for
// s3_bucket_posture.
func TestParity8852S3PostureContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8852Fixture(t)
	boundary := parity8852Boundary(fixture, catalog.ServiceS3)
	in := fixture.S3Posture
	env, err := NewS3BucketPostureEnvelope(S3BucketPostureObservation{
		Boundary:                 boundary,
		BucketARN:                in.BucketARN,
		BucketName:               in.BucketName,
		VersioningStatus:         in.VersioningStatus,
		VersioningEnabled:        in.VersioningEnabled,
		DefaultEncryptionEnabled: true,
		EncryptionAlgorithms:     []string{"aws:kms"},
		SSEKMSKeyARN:             in.SSEKMSKeyARN,
		LoggingTargetBucket:      in.LoggingTargetBucket,
	})
	if err != nil {
		t.Fatalf("NewS3BucketPostureEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, cloud.S3BucketPostureFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.SchemaVersion, cloud.S3BucketPostureSchemaVersionV1; got != want {
		t.Fatalf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := env.Payload["bucket_name"], in.BucketName; got != want {
		t.Fatalf("Payload[bucket_name] = %#v, want %#v", got, want)
	}
	if got, want := env.StableFactKey, parity8852S3PostureKey; got != want {
		t.Fatalf("StableFactKey = %q, want frozen %q", got, want)
	}
}

// TestParity8852IAMPermissionContract pins the in-tree side for
// aws_iam_permission: normalized actions and condition keys only — condition
// values must never reach the payload.
func TestParity8852IAMPermissionContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8852Fixture(t)
	boundary := parity8852Boundary(fixture, catalog.ServiceIAM)
	in := fixture.IAMPermission
	env, err := NewIAMPermissionEnvelope(IAMPermissionObservation{
		Boundary:           boundary,
		PrincipalARN:       in.PrincipalARN,
		PrincipalType:      in.PrincipalType,
		PolicySource:       in.PolicySource,
		PolicyName:         in.PolicyName,
		StatementSID:       in.StatementSID,
		Effect:             in.Effect,
		Actions:            in.Actions,
		Resources:          in.Resources,
		ConditionKeys:      in.ConditionKeys,
		ConditionOperators: in.ConditionOperators,
	})
	if err != nil {
		t.Fatalf("NewIAMPermissionEnvelope() error = %v, want nil", err)
	}
	if got, want := env.FactKind, cloud.AWSIAMPermissionFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := env.StableFactKey, parity8852IAMPermissionKey; got != want {
		t.Fatalf("StableFactKey = %q, want frozen %q", got, want)
	}
	raw := string(parity8852CanonicalJSON(t, env.Payload))
	for _, forbidden := range in.ConditionValuesForbidden {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("canonical payload bytes contain condition value %q, want keys only", forbidden)
		}
	}
}

// TestParity8852WarningContracts pins the in-tree side for aws_warning
// across the representative failure classes: credential acquisition failure,
// sustained throttling with omitted metadata (truncated page), and budget
// exhaustion (paginated scan yielded early and resumable).
func TestParity8852WarningContracts(t *testing.T) {
	t.Parallel()

	fixture := loadParity8852Fixture(t)
	boundary := parity8852Boundary(fixture, catalog.ServiceIAM)
	cases := map[string]string{
		"assumerole_failed":  parity8852AssumeRoleKey,
		"throttle_sustained": parity8852ThrottleKey,
		"budget_exhausted":   parity8852BudgetKey,
	}
	for name, warning := range fixture.Warnings {
		wantKey, ok := cases[name]
		if !ok {
			t.Fatalf("fixture warning %q has no frozen-key case", name)
		}
		env, err := NewWarningEnvelope(WarningObservation{
			Boundary:    boundary,
			WarningKind: warning.WarningKind,
			ErrorClass:  warning.ErrorClass,
			Message:     warning.Message,
		})
		if err != nil {
			t.Fatalf("NewWarningEnvelope(%s) error = %v, want nil", name, err)
		}
		if got, want := env.FactKind, cloud.AWSWarningFactKind; got != want {
			t.Fatalf("FactKind(%s) = %q, want %q", name, got, want)
		}
		if got, want := env.SchemaVersion, cloud.AWSWarningSchemaVersion; got != want {
			t.Fatalf("SchemaVersion(%s) = %q, want %q", name, got, want)
		}
		if got, want := env.Payload["warning_kind"], warning.WarningKind; got != want {
			t.Fatalf("Payload[warning_kind](%s) = %#v, want %#v", name, got, want)
		}
		if got, want := env.Payload["error_class"], warning.ErrorClass; got != want {
			t.Fatalf("Payload[error_class](%s) = %#v, want %#v", name, got, want)
		}
		if got, want := env.StableFactKey, wantKey; got != want {
			t.Fatalf("StableFactKey(%s) = %q, want frozen %q", name, got, want)
		}
	}

	// Warning identity is message-independent: the same failure re-observed
	// with different detail collapses onto the same fact.
	first, err := NewWarningEnvelope(WarningObservation{
		Boundary:    boundary,
		WarningKind: WarningAssumeRoleFailed,
		ErrorClass:  "credential_acquisition_failed",
	})
	if err != nil {
		t.Fatalf("NewWarningEnvelope() error = %v, want nil", err)
	}
	second, err := NewWarningEnvelope(WarningObservation{
		Boundary:    boundary,
		WarningKind: WarningAssumeRoleFailed,
		ErrorClass:  "credential_acquisition_failed",
		Message:     "different redacted detail",
	})
	if err != nil {
		t.Fatalf("NewWarningEnvelope() second error = %v, want nil", err)
	}
	if first.FactID != second.FactID {
		t.Fatalf("warning FactID changed with message: %q != %q", first.FactID, second.FactID)
	}
}

// TestParity8852NilSchemaOmitsOptionals proves sparse provider records still
// emit: nil tags/attributes, a DNS record with no alias target or TTL, and
// an S3 posture with unknown (nil) flags must not fail.
func TestParity8852NilSchemaOmitsOptionals(t *testing.T) {
	t.Parallel()

	fixture := loadParity8852Fixture(t)
	if _, err := NewResourceEnvelope(ResourceObservation{
		Boundary:     parity8852Boundary(fixture, catalog.ServiceIAM),
		ARN:          fixture.Resource.ARN,
		ResourceType: fixture.Resource.ResourceType,
	}); err != nil {
		t.Fatalf("NewResourceEnvelope(nil tags/attributes) error = %v, want nil", err)
	}
	env, err := NewDNSRecordEnvelope(DNSRecordObservation{
		Boundary:     parity8852Boundary(fixture, catalog.ServiceRoute53),
		HostedZoneID: fixture.DNSRecord.HostedZoneID,
		RecordName:   fixture.DNSRecord.RecordName,
		RecordType:   fixture.DNSRecord.RecordType,
	})
	if err != nil {
		t.Fatalf("NewDNSRecordEnvelope(nil alias/TTL) error = %v, want nil", err)
	}
	// Alias target is not part of the stable key (identity is
	// zone + normalized name + type), so the sparse record keeps the
	// fixture key; the payload must still show the alias as absent.
	if got, want := env.StableFactKey, parity8852DNSRecordKey; got != want {
		t.Fatalf("StableFactKey(nil alias) = %q, want fixture %q", got, want)
	}
	if got, want := env.Payload["has_alias_target"], false; got != want {
		t.Fatalf("Payload[has_alias_target](nil alias) = %#v, want %#v", got, want)
	}
	if _, ok := env.Payload["alias_target"]; ok {
		t.Fatalf("Payload[alias_target](nil alias) present, want omitted")
	}
	if _, err := NewS3BucketPostureEnvelope(S3BucketPostureObservation{
		Boundary:   parity8852Boundary(fixture, catalog.ServiceS3),
		BucketARN:  fixture.S3Posture.BucketARN,
		BucketName: fixture.S3Posture.BucketName,
	}); err != nil {
		t.Fatalf("NewS3BucketPostureEnvelope(nil flags) error = %v, want nil", err)
	}
}

// TestParity8852RedactionFailsClosed proves secret-shaped provider values are
// redacted before emission and that scan-status failure detail carries no raw
// AWS identifiers.
func TestParity8852RedactionFailsClosed(t *testing.T) {
	t.Parallel()

	fixture := loadParity8852Fixture(t)
	key := parity8852RedactKey(t)
	in := fixture.Redaction

	redacted, marker := ClassifyStackOutput(in.SecretOutputKey, in.SecretOutputValue, key)
	if !redacted {
		t.Fatalf("ClassifyStackOutput(%q) = false, want redaction of secret-shaped key", in.SecretOutputKey)
	}
	if marker == nil {
		t.Fatal("ClassifyStackOutput(secret) marker is nil, want redaction payload")
	}
	markerValue, _ := marker["marker"].(string)
	if strings.Contains(markerValue, in.SecretOutputValue) {
		t.Fatalf("redaction marker carries raw secret %q", in.SecretOutputValue)
	}

	if redacted, marker := ClassifyStackOutput(in.PlainOutputKey, in.PlainOutputValue, key); redacted || marker != nil {
		t.Fatalf("ClassifyStackOutput(%q) = (%v, %#v), want (false, nil) for non-secret key", in.PlainOutputKey, redacted, marker)
	}

	sanitized := SanitizeScanStatusMessage(in.StatusMessageSecrets)
	for _, leaked := range []string{"123456789012", "arn:aws:iam", "AKIAIOSFODNN7EXAMPLE", "123e4567-e89b-12d3-a456-426614174000"} {
		if strings.Contains(sanitized, leaked) {
			t.Fatalf("SanitizeScanStatusMessage() leaks %q: %q", leaked, sanitized)
		}
	}

	long := strings.Repeat("x", MaxScanStatusMessageLength+100)
	bounded := SanitizeScanStatusMessage(long)
	if len(bounded) > MaxScanStatusMessageLength {
		t.Fatalf("len(SanitizeScanStatusMessage(long)) = %d, want <= %d", len(bounded), MaxScanStatusMessageLength)
	}
	if !strings.HasSuffix(bounded, "...") {
		t.Fatalf("SanitizeScanStatusMessage(long) = %q, want truncation marker", bounded)
	}
}

// TestParity8852HostileInputsFailsClosed proves blank provider identity and
// blank envelope context are rejected, never emitted.
func TestParity8852HostileInputsFailsClosed(t *testing.T) {
	t.Parallel()

	fixture := loadParity8852Fixture(t)
	boundary := parity8852Boundary(fixture, catalog.ServiceIAM)
	hostile := fixture.Hostile

	if _, err := NewResourceEnvelope(ResourceObservation{
		Boundary:     boundary,
		ARN:          hostile.BlankARN,
		ResourceID:   hostile.BlankResourceID,
		ResourceType: fixture.Resource.ResourceType,
	}); err == nil {
		t.Fatal("NewResourceEnvelope(blank identity) error = nil, want rejection")
	}
	if _, err := NewResourceEnvelope(ResourceObservation{
		Boundary: boundary,
		ARN:      fixture.Resource.ARN,
	}); err == nil {
		t.Fatal("NewResourceEnvelope(blank resource_type) error = nil, want rejection")
	}
	if _, err := NewDNSRecordEnvelope(DNSRecordObservation{
		Boundary:     parity8852Boundary(fixture, catalog.ServiceRoute53),
		HostedZoneID: fixture.DNSRecord.HostedZoneID,
		RecordName:   hostile.BlankRecordName,
		RecordType:   fixture.DNSRecord.RecordType,
	}); err == nil {
		t.Fatal("NewDNSRecordEnvelope(blank record_name) error = nil, want rejection")
	}
	if _, err := NewIAMPermissionEnvelope(IAMPermissionObservation{
		Boundary:     boundary,
		PrincipalARN: hostile.BlankPrincipalARN,
		Effect:       fixture.IAMPermission.Effect,
		PolicySource: fixture.IAMPermission.PolicySource,
	}); err == nil {
		t.Fatal("NewIAMPermissionEnvelope(blank principal) error = nil, want rejection")
	}
	if _, err := NewWarningEnvelope(WarningObservation{Boundary: boundary}); err == nil {
		t.Fatal("NewWarningEnvelope(blank warning_kind) error = nil, want rejection")
	}
	blankScope := boundary
	blankScope.ScopeID = ""
	if _, err := NewResourceEnvelope(ResourceObservation{
		Boundary:     blankScope,
		ARN:          fixture.Resource.ARN,
		ResourceType: fixture.Resource.ResourceType,
	}); err == nil {
		t.Fatal("NewResourceEnvelope(blank scope) error = nil, want rejection")
	}
	blankGeneration := boundary
	blankGeneration.GenerationID = ""
	if _, err := NewRelationshipEnvelope(RelationshipObservation{
		Boundary:         blankGeneration,
		RelationshipType: fixture.Relationship.RelationshipType,
		SourceARN:        fixture.Relationship.SourceARN,
		TargetARN:        fixture.Relationship.TargetARN,
	}); err == nil {
		t.Fatal("NewRelationshipEnvelope(blank generation) error = nil, want rejection")
	}
	blankInstance := parity8852Boundary(fixture, catalog.ServiceRoute53)
	blankInstance.CollectorInstanceID = ""
	if _, err := NewDNSRecordEnvelope(DNSRecordObservation{
		Boundary:     blankInstance,
		HostedZoneID: fixture.DNSRecord.HostedZoneID,
		RecordName:   fixture.DNSRecord.RecordName,
		RecordType:   fixture.DNSRecord.RecordType,
	}); err == nil {
		t.Fatal("NewDNSRecordEnvelope(blank instance) error = nil, want rejection")
	}
	zeroFence := boundary
	zeroFence.FencingToken = 0
	if _, err := NewResourceEnvelope(ResourceObservation{
		Boundary:     zeroFence,
		ARN:          fixture.Resource.ARN,
		ResourceType: fixture.Resource.ResourceType,
	}); err == nil {
		t.Fatal("NewResourceEnvelope(zero fence) error = nil, want rejection")
	}
}
