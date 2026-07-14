// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testRDSAccount  = "111111111111"
	testRDSRegion   = "us-east-1"
	testRDSInstance = "aws_rds_db_instance"
	testRDSCluster  = "aws_rds_db_cluster"
)

func rdsResourceEnvelope(resourceType, arn, identifier string) facts.Envelope {
	return facts.Envelope{
		FactID:   "fact-resource-" + identifier,
		FactKind: facts.AWSResourceFactKind,
		Payload: map[string]any{
			"account_id":    testRDSAccount,
			"region":        testRDSRegion,
			"resource_type": resourceType,
			"resource_id":   arn,
			"arn":           arn,
			"name":          identifier,
		},
	}
}

func rdsPostureEnvelope(resourceType, arn, identifier string, public bool) facts.Envelope {
	return facts.Envelope{
		FactID:   "fact-posture-" + identifier,
		FactKind: facts.RDSInstancePostureFactKind,
		Payload: map[string]any{
			"account_id":                          testRDSAccount,
			"region":                              testRDSRegion,
			"resource_type":                       resourceType,
			"resource_id":                         arn,
			"arn":                                 arn,
			"identifier":                          identifier,
			"engine":                              "postgres",
			"publicly_accessible":                 public,
			"storage_encrypted":                   true,
			"kms_key_id":                          "arn:aws:kms:us-east-1:111111111111:key/db",
			"iam_database_authentication_enabled": true,
			"multi_az":                            true,
			"deletion_protection":                 true,
			"backup_retention_period":             int32(7),
			"performance_insights_enabled":        true,
			"performance_insights_retention_days": int32(31),
			"performance_insights_kms_key_id":     "arn:aws:kms:us-east-1:111111111111:key/pi",
			"ca_certificate_identifier":           "rds-ca-rsa2048-g1",
			"parameter_groups":                    []string{"orders-params", "orders-params"},
			"option_groups":                       []string{"orders-options"},
			"security_parameters": map[string]any{
				"rds.force_ssl": "1",
				"tls_version":   "TLSv1.2",
			},
		},
	}
}

func rdsUID(resourceType, arn string) string {
	return cloudResourceUID(testRDSAccount, testRDSRegion, resourceType, arn)
}

func TestExtractRDSPostureRowsProjectsInstanceAndCluster(t *testing.T) {
	t.Parallel()

	instanceARN := "arn:aws:rds:us-east-1:111111111111:db:orders-writer"
	clusterARN := "arn:aws:rds:us-east-1:111111111111:cluster:orders"
	resources := []facts.Envelope{
		rdsResourceEnvelope(testRDSInstance, instanceARN, "orders-writer"),
		rdsResourceEnvelope(testRDSCluster, clusterARN, "orders"),
	}
	postures := []facts.Envelope{
		rdsPostureEnvelope(testRDSInstance, instanceARN, "orders-writer", true),
		rdsPostureEnvelope(testRDSCluster, clusterARN, "orders", false),
	}

	rows, tally, _, err := ExtractRDSPostureRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractRDSPostureRows() error = %v, want nil", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if tally.updated != 2 {
		t.Fatalf("tally.updated = %d, want 2", tally.updated)
	}
	if tally.skipped[rdsPostureSkipSourceUnresolved] != 0 {
		t.Fatalf("source unresolved skips = %d, want 0", tally.skipped[rdsPostureSkipSourceUnresolved])
	}

	rowsByUID := map[string]map[string]any{}
	for _, row := range rows {
		rowsByUID[row["uid"].(string)] = row
	}

	instance := rowsByUID[rdsUID(testRDSInstance, instanceARN)]
	if instance == nil {
		t.Fatalf("missing instance row for uid %s in %#v", rdsUID(testRDSInstance, instanceARN), rows)
	}
	if got, want := instance["rds_public_exposure_state"], "candidate_public_endpoint"; got != want {
		t.Fatalf("public exposure state = %v, want %v", got, want)
	}
	if got, want := instance["rds_storage_encrypted"], true; got != want {
		t.Fatalf("storage encrypted = %v, want %v", got, want)
	}
	if got, want := instance["rds_iam_database_authentication_enabled"], true; got != want {
		t.Fatalf("iam db auth = %v, want %v", got, want)
	}
	if got, want := instance["rds_backup_retention_period"], int64(7); got != want {
		t.Fatalf("backup retention = %v, want %v", got, want)
	}
	params, ok := instance["rds_parameter_groups"].([]string)
	if !ok || len(params) != 1 || params[0] != "orders-params" {
		t.Fatalf("parameter groups = %#v, want one deduped orders-params", instance["rds_parameter_groups"])
	}
	security, ok := instance["rds_security_parameters"].([]string)
	if !ok || len(security) != 2 || security[0] != "rds.force_ssl=1" || security[1] != "tls_version=TLSv1.2" {
		t.Fatalf("security parameters = %#v, want deterministic key=value list", instance["rds_security_parameters"])
	}
	if got, want := instance["source_fact_id"], "fact-posture-orders-writer"; got != want {
		t.Fatalf("source fact id = %v, want %v", got, want)
	}

	cluster := rowsByUID[rdsUID(testRDSCluster, clusterARN)]
	if cluster == nil {
		t.Fatalf("missing cluster row for uid %s in %#v", rdsUID(testRDSCluster, clusterARN), rows)
	}
	if got, want := cluster["rds_public_exposure_state"], "not_public_endpoint"; got != want {
		t.Fatalf("cluster public exposure state = %v, want %v", got, want)
	}
}

func TestExtractRDSPostureRowsSkipsUnscannedResource(t *testing.T) {
	t.Parallel()

	instanceARN := "arn:aws:rds:us-east-1:111111111111:db:orders-writer"
	rows, tally, _, err := ExtractRDSPostureRows(nil, []facts.Envelope{
		rdsPostureEnvelope(testRDSInstance, instanceARN, "orders-writer", true),
	})
	if err != nil {
		t.Fatalf("ExtractRDSPostureRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 when posture source did not scan as a CloudResource", len(rows))
	}
	if got := tally.skipped[rdsPostureSkipSourceUnresolved]; got != 1 {
		t.Fatalf("source unresolved skips = %d, want 1", got)
	}
}

func TestExtractRDSPostureRowsDuplicateFactIsIdempotent(t *testing.T) {
	t.Parallel()

	instanceARN := "arn:aws:rds:us-east-1:111111111111:db:orders-writer"
	resources := []facts.Envelope{rdsResourceEnvelope(testRDSInstance, instanceARN, "orders-writer")}
	postures := []facts.Envelope{
		rdsPostureEnvelope(testRDSInstance, instanceARN, "orders-writer", true),
		rdsPostureEnvelope(testRDSInstance, instanceARN, "orders-writer", true),
	}

	rows, tally, _, err := ExtractRDSPostureRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractRDSPostureRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 deduped posture row", len(rows))
	}
	if tally.updated != 1 {
		t.Fatalf("tally.updated = %d, want 1", tally.updated)
	}
}

func TestExtractRDSPostureRowsEmptyInputIsNoOp(t *testing.T) {
	t.Parallel()

	rows, tally, _, err := ExtractRDSPostureRows(nil, nil)
	if err != nil {
		t.Fatalf("ExtractRDSPostureRows() error = %v, want nil", err)
	}
	if rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped() = %d, want 0", tally.totalSkipped())
	}
}

// TestExtractRDSPostureRowsQuarantinesMissingRequiredField proves an
// rds_instance_posture fact MISSING a required identity key (account_id
// absent, not empty) is quarantined as an input_invalid per-fact dead-letter
// and produces no row, while a valid sibling fact in the same batch still
// projects normally. Before the typed-decode migration, rdsPostureRow read
// account_id via a raw payloadString lookup that silently defaulted a missing
// key to "", so the malformed fact would have fabricated a CloudResource row
// keyed by an empty account_id instead of dead-lettering — this test locks
// the corrected behavior (#4632).
func TestExtractRDSPostureRowsQuarantinesMissingRequiredField(t *testing.T) {
	t.Parallel()

	instanceARN := "arn:aws:rds:us-east-1:111111111111:db:orders-writer"
	resources := []facts.Envelope{
		rdsResourceEnvelope(testRDSInstance, instanceARN, "orders-writer"),
	}
	malformed := facts.Envelope{
		FactID:   "fact-posture-missing-account",
		FactKind: facts.RDSInstancePostureFactKind,
		Payload: map[string]any{
			// account_id intentionally absent.
			"region":                              testRDSRegion,
			"resource_type":                       testRDSInstance,
			"resource_id":                         instanceARN,
			"arn":                                 instanceARN,
			"identifier":                          "orders-writer",
			"engine":                              "postgres",
			"publicly_accessible":                 true,
			"storage_encrypted":                   true,
			"iam_database_authentication_enabled": true,
			"multi_az":                            true,
			"deletion_protection":                 true,
			"backup_retention_period":             int32(7),
			"performance_insights_enabled":        true,
			"performance_insights_retention_days": int32(31),
		},
	}
	postures := []facts.Envelope{
		malformed,
		rdsPostureEnvelope(testRDSInstance, instanceARN, "orders-writer", true),
	}

	rows, tally, quarantined, err := ExtractRDSPostureRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractRDSPostureRows() error = %v, want nil (per-fact isolation, not batch abort)", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; the valid fact must still project despite the malformed sibling", len(rows))
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-account_id fact must be quarantined", len(quarantined))
	}
	if quarantined[0].factKind != facts.RDSInstancePostureFactKind {
		t.Fatalf("quarantined factKind = %q, want %q", quarantined[0].factKind, facts.RDSInstancePostureFactKind)
	}
	if quarantined[0].field != "account_id" {
		t.Fatalf("quarantined field = %q, want %q", quarantined[0].field, "account_id")
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined classification = %q, want %q", quarantined[0].classification, "input_invalid")
	}
	if tally.updated != 1 {
		t.Fatalf("tally.updated = %d, want 1", tally.updated)
	}
	for _, row := range rows {
		if row["uid"] == cloudResourceUID("", testRDSRegion, testRDSInstance, instanceARN) {
			t.Fatalf("found a row with a fabricated empty-account_id uid: %#v", row)
		}
	}
}
