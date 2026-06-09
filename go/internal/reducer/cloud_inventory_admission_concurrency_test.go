package reducer

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
)

// convergentFactStore simulates the canonicalReducerFactInsertQuery
// ON CONFLICT (fact_id) DO UPDATE upsert by keying rows on the fact id (arg 0).
// Two workers writing the same fact id converge to one row, exactly like the
// Postgres primary-key conflict the real query relies on.
type convergentFactStore struct {
	mu   sync.Mutex
	rows map[string][]any
}

func newConvergentFactStore() *convergentFactStore {
	return &convergentFactStore{rows: make(map[string][]any)}
}

func (s *convergentFactStore) ExecContext(_ context.Context, _ string, args ...any) (sql.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	factID, _ := args[0].(string)
	s.rows[factID] = args
	return fakeWorkloadIdentityResult{}, nil
}

func (s *convergentFactStore) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

func TestCloudInventoryAdmissionConcurrentWorkersConverge(t *testing.T) {
	t.Parallel()

	store := newConvergentFactStore()
	records := []CloudInventoryRecord{
		{
			Provider:    cloudinventory.ProviderGCP,
			FactKind:    "gcp_cloud_resource",
			RawIdentity: "//compute.googleapis.com/projects/eshu-prod/zones/us-central1-a/instances/api-1",
			SourceLayer: SourceLayerObserved,
		},
		{
			Provider:    cloudinventory.ProviderAzure,
			FactKind:    "azure_cloud_resource",
			RawIdentity: "/subscriptions/0000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/api-1",
			SourceLayer: SourceLayerObserved,
		},
	}

	newHandler := func() CloudInventoryAdmissionHandler {
		return CloudInventoryAdmissionHandler{
			EvidenceLoader: &stubCloudInventoryEvidenceLoader{records: records},
			Writer:         PostgresCloudInventoryAdmissionWriter{DB: store},
		}
	}
	intent := cloudInventoryIntent()

	var wg sync.WaitGroup
	const workers = 8
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = newHandler().Handle(context.Background(), intent)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d Handle() error = %v", i, err)
		}
	}

	// Two distinct uids admitted by the same generation across 8 concurrent
	// workers must converge to exactly two canonical rows, not 16.
	if got, want := store.rowCount(), 2; got != want {
		t.Fatalf("convergent canonical rows = %d, want %d (no MERGE/duplicate races)", got, want)
	}
}
