// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestNewRDSInstancePostureEnvelopeCarriesDerivedPosture(t *testing.T) {
	t.Parallel()

	boundary := testBoundary(time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC))
	boundary.ServiceKind = ServiceRDS
	observation := RDSPostureObservation{
		Boundary:                         boundary,
		ARN:                              "arn:aws:rds:us-east-1:123456789012:db:orders-writer",
		ResourceID:                       "db-ORDERSWRITER",
		ResourceType:                     ResourceTypeRDSDBInstance,
		Identifier:                       "orders-writer",
		Engine:                           "postgres",
		PubliclyAccessible:               true,
		StorageEncrypted:                 true,
		KMSKeyID:                         "arn:aws:kms:us-east-1:123456789012:key/orders",
		IAMDatabaseAuthenticationEnabled: true,
		MultiAZ:                          true,
		DeletionProtection:               false,
		BackupRetentionPeriod:            7,
		PerformanceInsightsEnabled:       true,
		PerformanceInsightsRetentionDays: 731,
		PerformanceInsightsKMSKeyID:      "arn:aws:kms:us-east-1:123456789012:key/pi",
		CACertificateIdentifier:          "rds-ca-rsa2048-g1",
		ParameterGroups:                  []string{"orders-postgres16"},
		OptionGroups:                     []string{"orders-options"},
		SecurityParameters: map[string]string{
			"rds.force_ssl":      "1",
			"log_connections":    "1",
			"log_disconnections": "1",
		},
	}

	envelope, err := NewRDSInstancePostureEnvelope(observation)
	if err != nil {
		t.Fatalf("NewRDSInstancePostureEnvelope() error = %v, want nil", err)
	}

	if envelope.FactKind != facts.RDSInstancePostureFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.RDSInstancePostureFactKind)
	}
	if envelope.SchemaVersion != facts.RDSPostureSchemaVersionV1 {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.RDSPostureSchemaVersionV1)
	}
	if envelope.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", envelope.SourceConfidence, facts.SourceConfidenceReported)
	}
	if envelope.SourceRef.SourceSystem != CollectorKind {
		t.Fatalf("SourceRef.SourceSystem = %q, want %q", envelope.SourceRef.SourceSystem, CollectorKind)
	}

	payload := envelope.Payload
	if got, want := payload["resource_type"], ResourceTypeRDSDBInstance; got != want {
		t.Fatalf("resource_type = %#v, want %q", got, want)
	}
	if got, want := payload["resource_id"], "db-ORDERSWRITER"; got != want {
		t.Fatalf("resource_id = %#v, want %q", got, want)
	}
	if got, want := payload["service_kind"], ServiceRDS; got != want {
		t.Fatalf("service_kind = %#v, want %q", got, want)
	}
	if got, want := payload["publicly_accessible"], true; got != want {
		t.Fatalf("publicly_accessible = %#v, want %v", got, want)
	}
	if got, want := payload["storage_encrypted"], true; got != want {
		t.Fatalf("storage_encrypted = %#v, want %v", got, want)
	}
	if got, want := payload["kms_key_id"], "arn:aws:kms:us-east-1:123456789012:key/orders"; got != want {
		t.Fatalf("kms_key_id = %#v, want %q", got, want)
	}
	if got, want := payload["iam_database_authentication_enabled"], true; got != want {
		t.Fatalf("iam_database_authentication_enabled = %#v, want %v", got, want)
	}
	if got, want := payload["backup_retention_period"], int32(7); got != want {
		t.Fatalf("backup_retention_period = %#v, want %v", got, want)
	}
	if got, want := payload["multi_az"], true; got != want {
		t.Fatalf("multi_az = %#v, want %v", got, want)
	}
	if got, want := payload["deletion_protection"], false; got != want {
		t.Fatalf("deletion_protection = %#v, want %v", got, want)
	}
	if got, want := payload["performance_insights_enabled"], true; got != want {
		t.Fatalf("performance_insights_enabled = %#v, want %v", got, want)
	}
	if got, want := payload["performance_insights_retention_days"], int32(731); got != want {
		t.Fatalf("performance_insights_retention_days = %#v, want %v", got, want)
	}
	if got, want := payload["performance_insights_kms_key_id"], "arn:aws:kms:us-east-1:123456789012:key/pi"; got != want {
		t.Fatalf("performance_insights_kms_key_id = %#v, want %q", got, want)
	}
	if got, want := payload["ca_certificate_identifier"], "rds-ca-rsa2048-g1"; got != want {
		t.Fatalf("ca_certificate_identifier = %#v, want %q", got, want)
	}
	parameterGroups, ok := payload["parameter_groups"].([]string)
	if !ok || len(parameterGroups) != 1 || parameterGroups[0] != "orders-postgres16" {
		t.Fatalf("parameter_groups = %#v, want [orders-postgres16]", payload["parameter_groups"])
	}
	optionGroups, ok := payload["option_groups"].([]string)
	if !ok || len(optionGroups) != 1 || optionGroups[0] != "orders-options" {
		t.Fatalf("option_groups = %#v, want [orders-options]", payload["option_groups"])
	}
	securityParameters, ok := payload["security_parameters"].(map[string]string)
	if !ok || securityParameters["rds.force_ssl"] != "1" {
		t.Fatalf("security_parameters = %#v, want rds.force_ssl=1", payload["security_parameters"])
	}

	anchors, ok := payload["correlation_anchors"].([]string)
	if !ok || len(anchors) == 0 {
		t.Fatalf("correlation_anchors = %#v, want non-empty", payload["correlation_anchors"])
	}

	for _, forbidden := range []string{
		"master_username",
		"password",
		"secret",
		"snapshot",
		"log_contents",
		"database_name",
		"performance_insights_samples",
	} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("%s persisted on posture fact; RDS posture must stay metadata-only", forbidden)
		}
	}
}

func TestNewRDSInstancePostureEnvelopeRequiresResourceIdentity(t *testing.T) {
	t.Parallel()

	boundary := testBoundary(time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC))
	boundary.ServiceKind = ServiceRDS
	_, err := NewRDSInstancePostureEnvelope(RDSPostureObservation{
		Boundary:     boundary,
		ResourceType: ResourceTypeRDSDBInstance,
	})
	if err == nil {
		t.Fatalf("NewRDSInstancePostureEnvelope() error = nil, want missing-identity error")
	}
}

func TestNewRDSInstancePostureEnvelopeRequiresResourceType(t *testing.T) {
	t.Parallel()

	boundary := testBoundary(time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC))
	boundary.ServiceKind = ServiceRDS
	_, err := NewRDSInstancePostureEnvelope(RDSPostureObservation{
		Boundary: boundary,
		ARN:      "arn:aws:rds:us-east-1:123456789012:db:orders-writer",
	})
	if err == nil {
		t.Fatalf("NewRDSInstancePostureEnvelope() error = nil, want missing-resource-type error")
	}
}

func TestNewRDSInstancePostureEnvelopeStableKeyDistinguishesInstanceAndCluster(t *testing.T) {
	t.Parallel()

	boundary := testBoundary(time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC))
	boundary.ServiceKind = ServiceRDS
	instance, err := NewRDSInstancePostureEnvelope(RDSPostureObservation{
		Boundary:     boundary,
		ResourceID:   "orders",
		ResourceType: ResourceTypeRDSDBInstance,
	})
	if err != nil {
		t.Fatalf("instance posture error = %v", err)
	}
	cluster, err := NewRDSInstancePostureEnvelope(RDSPostureObservation{
		Boundary:     boundary,
		ResourceID:   "orders",
		ResourceType: ResourceTypeRDSDBCluster,
	})
	if err != nil {
		t.Fatalf("cluster posture error = %v", err)
	}
	if instance.StableFactKey == cluster.StableFactKey {
		t.Fatalf("instance and cluster posture share StableFactKey %q", instance.StableFactKey)
	}
}
