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
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	if db.execs[0].args[0] == db.execs[1].args[0] {
		t.Fatalf("fact ids must differ per uid: %v", db.execs[0].args[0])
	}
	if got, want := db.execs[0].args[3], cloudInventoryAdmissionFactKind; got != want {
		t.Fatalf("fact_kind = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[6], facts.SourceConfidenceInferred; got != want {
		t.Fatalf("source_confidence = %v, want %v", got, want)
	}

	payloadBytes, ok := db.execs[1].args[14].([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", db.execs[1].args[14])
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
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
	half := len(db.execs) / 2
	for i := 0; i < half; i++ {
		if db.execs[i].args[0] != db.execs[i+half].args[0] {
			t.Fatalf("fact id not stable across re-admission at %d: %v vs %v", i, db.execs[i].args[0], db.execs[i+half].args[0])
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
