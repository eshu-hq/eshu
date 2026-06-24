// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rds

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsInstanceAndClusterPostureFacts(t *testing.T) {
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	piKMSARN := "arn:aws:kms:us-east-1:123456789012:key/pi"
	client := fakeClient{
		instances: []DBInstance{{
			ARN:                              "arn:aws:rds:us-east-1:123456789012:db:orders-writer",
			Identifier:                       "orders-writer",
			ResourceID:                       "db-ORDERSWRITER",
			Engine:                           "postgres",
			Status:                           "available",
			PubliclyAccessible:               true,
			StorageEncrypted:                 true,
			KMSKeyID:                         kmsARN,
			IAMDatabaseAuthenticationEnabled: true,
			MultiAZ:                          true,
			DeletionProtection:               false,
			BackupRetentionPeriod:            7,
			PerformanceInsightsEnabled:       true,
			PerformanceInsightsRetentionDays: 731,
			PerformanceInsightsKMSKeyID:      piKMSARN,
			CACertificateIdentifier:          "rds-ca-rsa2048-g1",
			ParameterGroups:                  []ParameterGroup{{Name: "orders-postgres16", State: "in-sync"}},
			OptionGroups:                     []OptionGroup{{Name: "orders-options", State: "in-sync"}},
			SecurityParameters:               map[string]string{"rds.force_ssl": "1"},
		}},
		clusters: []DBCluster{{
			ARN:                              "arn:aws:rds:us-east-1:123456789012:cluster:orders",
			Identifier:                       "orders",
			ResourceID:                       "cluster-ORDERS",
			Engine:                           "aurora-postgresql",
			Status:                           "available",
			StorageEncrypted:                 true,
			KMSKeyID:                         kmsARN,
			IAMDatabaseAuthenticationEnabled: true,
			MultiAZ:                          true,
			DeletionProtection:               true,
			BackupRetentionPeriod:            14,
			PerformanceInsightsEnabled:       true,
			PerformanceInsightsRetentionDays: 7,
			PerformanceInsightsKMSKeyID:      piKMSARN,
			ParameterGroup:                   "orders-cluster-params",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	instancePosture := postureByResourceType(t, envelopes, awscloud.ResourceTypeRDSDBInstance)
	assertPosture(t, instancePosture, "publicly_accessible", true)
	assertPosture(t, instancePosture, "storage_encrypted", true)
	assertPosture(t, instancePosture, "kms_key_id", kmsARN)
	assertPosture(t, instancePosture, "iam_database_authentication_enabled", true)
	assertPosture(t, instancePosture, "backup_retention_period", int32(7))
	assertPosture(t, instancePosture, "multi_az", true)
	assertPosture(t, instancePosture, "deletion_protection", false)
	assertPosture(t, instancePosture, "performance_insights_enabled", true)
	assertPosture(t, instancePosture, "performance_insights_retention_days", int32(731))
	assertPosture(t, instancePosture, "performance_insights_kms_key_id", piKMSARN)
	assertPosture(t, instancePosture, "ca_certificate_identifier", "rds-ca-rsa2048-g1")
	if got, _ := instancePosture.Payload["parameter_groups"].([]string); len(got) != 1 || got[0] != "orders-postgres16" {
		t.Fatalf("instance parameter_groups = %#v, want [orders-postgres16]", instancePosture.Payload["parameter_groups"])
	}
	if got, _ := instancePosture.Payload["option_groups"].([]string); len(got) != 1 || got[0] != "orders-options" {
		t.Fatalf("instance option_groups = %#v, want [orders-options]", instancePosture.Payload["option_groups"])
	}
	if got, _ := instancePosture.Payload["security_parameters"].(map[string]string); got["rds.force_ssl"] != "1" {
		t.Fatalf("instance security_parameters = %#v, want rds.force_ssl=1", instancePosture.Payload["security_parameters"])
	}
	if got, want := instancePosture.Payload["service_kind"], awscloud.ServiceRDS; got != want {
		t.Fatalf("instance posture service_kind = %#v, want %q", got, want)
	}

	clusterPosture := postureByResourceType(t, envelopes, awscloud.ResourceTypeRDSDBCluster)
	assertPosture(t, clusterPosture, "storage_encrypted", true)
	assertPosture(t, clusterPosture, "deletion_protection", true)
	assertPosture(t, clusterPosture, "backup_retention_period", int32(14))
	assertPosture(t, clusterPosture, "performance_insights_enabled", true)
	assertPosture(t, clusterPosture, "performance_insights_retention_days", int32(7))
	if got, _ := clusterPosture.Payload["parameter_groups"].([]string); len(got) != 1 || got[0] != "orders-cluster-params" {
		t.Fatalf("cluster parameter_groups = %#v, want [orders-cluster-params]", clusterPosture.Payload["parameter_groups"])
	}

	for _, posture := range []facts.Envelope{instancePosture, clusterPosture} {
		for _, forbidden := range []string{
			"master_username",
			"password",
			"secret",
			"snapshot",
			"log_contents",
			"database_name",
			"performance_insights_samples",
		} {
			if _, exists := posture.Payload[forbidden]; exists {
				t.Fatalf("%s persisted on posture fact; RDS posture must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerEmitsNoPostureRelationships(t *testing.T) {
	client := fakeClient{
		instances: []DBInstance{{
			ARN:        "arn:aws:rds:us-east-1:123456789012:db:orders-writer",
			Identifier: "orders-writer",
			ResourceID: "db-ORDERSWRITER",
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.RDSInstancePostureFactKind {
			continue
		}
		if _, exists := envelope.Payload["relationship_type"]; exists {
			t.Fatalf("posture fact carried a relationship_type; PR1 posture is facts-only")
		}
	}
}

func postureByResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.RDSInstancePostureFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing rds_instance_posture for resource_type %q", resourceType)
	return facts.Envelope{}
}

func assertPosture(t *testing.T, envelope facts.Envelope, key string, want any) {
	t.Helper()
	got, exists := envelope.Payload[key]
	if !exists {
		t.Fatalf("missing posture field %q in %#v", key, envelope.Payload)
	}
	if got != want {
		t.Fatalf("posture field %q = %#v, want %#v", key, got, want)
	}
}
