package reducer

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
)

// TestWriteCloudInventoryAdmissionBoundedExecCount guards issue #3435: admitted
// canonical identities must be persisted in O(N/batchSize) bulk inserts rather
// than one ExecContext per resource.
func TestWriteCloudInventoryAdmissionBoundedExecCount(t *testing.T) {
	t.Parallel()

	const resourceCount = 400
	resources := make([]AdmittedCloudResource, resourceCount)
	for i := range resources {
		resources[i] = AdmittedCloudResource{
			CloudResourceUID:    fmt.Sprintf("cloud_resource:%d", i),
			Provider:            cloudinventory.ProviderGCP,
			RawIdentity:         fmt.Sprintf("//compute.googleapis.com/projects/eshu-prod/instances/api-%d", i),
			ResourceType:        "compute.googleapis.com/Instance",
			FactKinds:           []string{"gcp_cloud_resource"},
			ManagementOrigin:    ManagementOriginObserved,
			HasObservedEvidence: true,
		}
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresCloudInventoryAdmissionWriter{DB: db}

	result, err := writer.WriteCloudInventoryAdmission(context.Background(), CloudInventoryAdmissionWrite{
		IntentID:     "intent-cloud-batch",
		ScopeID:      "gcp:org:eshu:project:prod",
		GenerationID: "gen-batch",
		SourceSystem: "gcp",
		Resources:    resources,
		Summary:      CloudInventoryAdmissionSummary{Admitted: resourceCount},
	})
	if err != nil {
		t.Fatalf("WriteCloudInventoryAdmission() error = %v", err)
	}
	if got, want := result.CanonicalWrites, resourceCount; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	wantExecs := expectedBatchedExecCount(resourceCount)
	if got := len(db.execs); got != wantExecs {
		t.Fatalf("ExecContext calls = %d for %d resources, want %d (bounded batched inserts)", got, resourceCount, wantExecs)
	}
	if rows := decodeBatchedFactCalls(t, db.execs); len(rows) != resourceCount {
		t.Fatalf("decoded rows = %d, want %d", len(rows), resourceCount)
	}
}
