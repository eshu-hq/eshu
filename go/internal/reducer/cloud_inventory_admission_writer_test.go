package reducer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func cloudInventoryWriteFixture() CloudInventoryAdmissionWrite {
	return CloudInventoryAdmissionWrite{
		IntentID:     "intent-cloud-inventory",
		ScopeID:      "gcp:org:eshu:project:prod",
		GenerationID: "generation-1",
		SourceSystem: "gcp",
		Cause:        "cloud inventory facts observed",
		Resources: []AdmittedCloudResource{
			{
				CloudResourceUID:    "cloud_resource:aaa",
				Provider:            cloudinventory.ProviderGCP,
				RawIdentity:         "//compute.googleapis.com/projects/eshu-prod/zones/us-central1-a/instances/api-1",
				ResourceType:        "compute.googleapis.com/Instance",
				FactKinds:           []string{"gcp_cloud_resource"},
				ManagementOrigin:    ManagementOriginObserved,
				HasObservedEvidence: true,
			},
			{
				CloudResourceUID:    "cloud_resource:bbb",
				Provider:            cloudinventory.ProviderAzure,
				RawIdentity:         "/subscriptions/0000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/api-1",
				ResourceType:        "Microsoft.Compute/virtualMachines",
				FactKinds:           []string{"azure_cloud_resource", "terraform_managed_resource"},
				ManagementOrigin:    ManagementOriginDeclared,
				HasDeclaredEvidence: true,
				HasObservedEvidence: true,
				IdentityPolicyEvidence: []CloudIdentityPolicyEvidence{
					{
						EvidenceKey:          "identity-stable-1",
						IdentityType:         "system_assigned",
						RoleClass:            "contributor",
						PrincipalFingerprint: "principal-marker",
						TenantFingerprint:    "tenant-marker",
					},
				},
				ResourceChangeEvidence: []CloudResourceChangeEvidence{
					{
						EvidenceKey:              "change-stable-1",
						ChangeType:               "deleted",
						ChangeTime:               time.Date(2026, time.June, 16, 10, 30, 0, 0, time.UTC),
						Operation:                "Microsoft.Compute/virtualMachines/delete",
						ClientType:               "AzurePortal",
						ActorClass:               "user",
						ActorFingerprint:         "actor-marker",
						ChangedPropertyPaths:     []string{"properties.provisioningState"},
						ChangedPropertyTruncated: true,
						TombstoneCandidate:       true,
					},
				},
			},
		},
		Summary: CloudInventoryAdmissionSummary{Admitted: 2, Ambiguous: 1},
	}
}

func TestPostgresCloudInventoryAdmissionWriterRequiresDatabase(t *testing.T) {
	t.Parallel()

	_, err := PostgresCloudInventoryAdmissionWriter{}.WriteCloudInventoryAdmission(
		context.Background(),
		cloudInventoryWriteFixture(),
	)
	if err == nil {
		t.Fatal("WriteCloudInventoryAdmission() error = nil, want missing database error")
	}
}

func TestPostgresCloudInventoryAdmissionWriterPersistsOneFactPerResource(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresCloudInventoryAdmissionWriter{DB: db, Now: func() time.Time { return now }}

	result, err := writer.WriteCloudInventoryAdmission(context.Background(), cloudInventoryWriteFixture())
	if err != nil {
		t.Fatalf("WriteCloudInventoryAdmission() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	// Two resources are now written in a single bounded batched insert, so the
	// writer issues one ExecContext call regardless of resource count.
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	rows := decodeBatchedFactCalls(t, db.execs)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}
	if rows[0].FactID == rows[1].FactID {
		t.Fatalf("fact ids must differ per uid: %v", rows[0].FactID)
	}
	if got, want := rows[0].FactKind, cloudInventoryAdmissionFactKind; got != want {
		t.Fatalf("fact_kind = %v, want %v", got, want)
	}
	if got, want := rows[0].SourceConfidence, facts.SourceConfidenceInferred; got != want {
		t.Fatalf("source_confidence = %v, want %v", got, want)
	}

	var payload map[string]any
	if err := json.Unmarshal(rows[1].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got, want := payload["management_origin"], string(ManagementOriginDeclared); got != want {
		t.Fatalf("management_origin = %#v, want %q", got, want)
	}
	if got, want := payload["cloud_resource_uid"], "cloud_resource:bbb"; got != want {
		t.Fatalf("cloud_resource_uid = %#v, want %q", got, want)
	}
	if payload["has_declared_evidence"] != true {
		t.Fatalf("has_declared_evidence = %#v, want true", payload["has_declared_evidence"])
	}
	evidence, ok := payload["identity_policy_evidence"].([]any)
	if !ok {
		t.Fatalf("identity_policy_evidence type = %T, want []any", payload["identity_policy_evidence"])
	}
	if len(evidence) != 1 {
		t.Fatalf("identity_policy_evidence length = %d, want 1", len(evidence))
	}
	row := evidence[0].(map[string]any)
	if row["principal_fingerprint"] != "principal-marker" || row["tenant_fingerprint"] != "tenant-marker" {
		t.Fatalf("identity_policy_evidence row = %#v", row)
	}
	if _, present := row["assignment_scope"]; present {
		t.Fatalf("raw assignment_scope must not be persisted: %#v", row)
	}
	changeEvidence, ok := payload["resource_change_freshness"].([]any)
	if !ok {
		t.Fatalf("resource_change_freshness type = %T, want []any", payload["resource_change_freshness"])
	}
	if len(changeEvidence) != 1 {
		t.Fatalf("resource_change_freshness length = %d, want 1", len(changeEvidence))
	}
	changeRow := changeEvidence[0].(map[string]any)
	if changeRow["change_type"] != "deleted" || changeRow["tombstone_candidate"] != true {
		t.Fatalf("resource change row = %#v, want deleted tombstone candidate", changeRow)
	}
	if changeRow["actor_fingerprint"] != "actor-marker" {
		t.Fatalf("actor_fingerprint = %#v, want actor marker", changeRow["actor_fingerprint"])
	}
	if _, present := changeRow["target_arm_resource_id"]; present {
		t.Fatalf("raw target identity must not be persisted in change evidence: %#v", changeRow)
	}
	if _, present := changeRow["changedBy"]; present {
		t.Fatalf("raw actor field must not be persisted in change evidence: %#v", changeRow)
	}
}

func TestPostgresCloudInventoryAdmissionWriterIsIdempotentByUID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresCloudInventoryAdmissionWriter{DB: db, Now: func() time.Time { return now }}
	write := cloudInventoryWriteFixture()

	first, err := writer.WriteCloudInventoryAdmission(context.Background(), write)
	if err != nil {
		t.Fatalf("first write error = %v", err)
	}
	second, err := writer.WriteCloudInventoryAdmission(context.Background(), write)
	if err != nil {
		t.Fatalf("second write error = %v", err)
	}

	// Re-admitting the same generation must target the same fact ids so the
	// ON CONFLICT upsert converges instead of duplicating canonical rows.
	if len(first.CanonicalIDs) != len(second.CanonicalIDs) {
		t.Fatalf("canonical id count drift: %d vs %d", len(first.CanonicalIDs), len(second.CanonicalIDs))
	}
	rows := decodeBatchedFactCalls(t, db.execs)
	half := len(rows) / 2
	for i := 0; i < half; i++ {
		if rows[i].FactID != rows[i+half].FactID {
			t.Fatalf("fact id not stable across re-admission at %d: %v vs %v", i, rows[i].FactID, rows[i+half].FactID)
		}
	}
}

func TestCloudInventoryAdmissionFactIDIsStableAndUIDPartitioned(t *testing.T) {
	t.Parallel()

	write := cloudInventoryWriteFixture()
	a := cloudInventoryAdmissionFactID(write, write.Resources[0])
	b := cloudInventoryAdmissionFactID(write, write.Resources[1])
	if a == b {
		t.Fatalf("fact id must partition by uid, got identical %q", a)
	}
	// Two workers admitting the same uid in the same generation derive the same
	// id, which is the conflict key that lets concurrent writers converge.
	again := cloudInventoryAdmissionFactID(write, write.Resources[0])
	if a != again {
		t.Fatalf("fact id not deterministic: %q vs %q", a, again)
	}
}
