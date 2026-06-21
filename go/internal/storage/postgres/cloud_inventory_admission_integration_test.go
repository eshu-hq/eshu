package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// cloudInventoryAdmissionIntent returns the canonical admission intent under
// test, bound to one scope generation.
func cloudInventoryAdmissionIntent() reducer.Intent {
	return reducer.Intent{
		IntentID:     "intent-cloud-inventory-1",
		ScopeID:      "cloud:tenant-1",
		GenerationID: "gen-1",
		SourceSystem: "reducer",
		Domain:       reducer.DomainCloudInventoryAdmission,
		Cause:        "cloud inventory facts observed",
	}
}

// cloudInventorySourceRows returns one source-fact row per provider for the
// loader query response, mirroring how the three provider collectors persist raw
// identity under their provider-specific payload keys.
func cloudInventorySourceRows() [][]any {
	awsARN := "arn:aws:s3:::managed-bucket"
	gcpName := "//compute.googleapis.com/projects/p/zones/z/instances/i"
	azureID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm"
	return [][]any{
		{facts.AWSResourceFactKind, awsARN, []byte(`{"arn":"` + awsARN + `","resource_type":"aws_s3_bucket"}`)},
		{facts.GCPCloudResourceFactKind, gcpName, []byte(`{"full_resource_name":"` + gcpName + `","asset_type":"compute.googleapis.com/Instance"}`)},
		{facts.AzureCloudResourceFactKind, azureID, []byte(`{"arm_resource_id":"` + azureID + `","resource_type":"microsoft.compute/virtualmachines"}`)},
	}
}

// newCloudInventoryAdmissionHandler wires the production loader and writer
// around the shared admission handler so the test exercises the real
// load -> resolve -> admit -> upsert path end to end against the fake database.
func newCloudInventoryAdmissionHandler(db *fakeExecQueryer) reducer.CloudInventoryAdmissionHandler {
	return reducer.CloudInventoryAdmissionHandler{
		EvidenceLoader: PostgresCloudInventoryEvidenceLoader{DB: db},
		Writer:         reducer.PostgresCloudInventoryAdmissionWriter{DB: db},
	}
}

func newCloudInventoryAdmissionHandlerWithFreshness(db *fakeExecQueryer) reducer.CloudInventoryAdmissionHandler {
	handler := newCloudInventoryAdmissionHandler(db)
	handler.ResourceChangeEvidenceLoader = PostgresCloudResourceChangeEvidenceLoader{DB: db}
	return handler
}

// TestCloudInventoryAdmissionEndToEndProducesCanonicalRows proves the full
// production path: the Postgres loader reads the three provider source fact
// kinds for one generation, the admission handler resolves each into the shared
// cloud_resource_uid keyspace, and the Postgres writer upserts one canonical
// reducer_cloud_resource_identity row per uid with observed management origin.
func TestCloudInventoryAdmissionEndToEndProducesCanonicalRows(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: cloudInventorySourceRows()}}}
	handler := newCloudInventoryAdmissionHandler(db)

	result, err := handler.Handle(context.Background(), cloudInventoryAdmissionIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != reducer.ResultStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, reducer.ResultStatusSucceeded)
	}
	if got, want := result.CanonicalWrites, 3; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	// The writer now batches all admitted resources into one bounded bulk insert,
	// so a single ExecContext call carries all three canonical rows.
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("canonical upserts = %d, want %d", got, want)
	}

	rows := decodeCloudInventoryBatchedRows(t, db.execs)
	if got, want := len(rows), 3; got != want {
		t.Fatalf("canonical rows = %d, want %d", got, want)
	}
	for _, row := range rows {
		// Every canonical write targets the reducer-owned fact kind on the
		// idempotent insert query.
		if got, want := row.factKind, "reducer_cloud_resource_identity"; got != want {
			t.Fatalf("canonical fact kind = %v, want %v", got, want)
		}
		payload := cloudInventoryDecodePayload(t, row.payload)
		if got, want := payload["management_origin"], "observed"; got != want {
			t.Fatalf("management_origin = %v, want %v", got, want)
		}
		if payload["has_observed_evidence"] != true {
			t.Fatalf("has_observed_evidence = %v, want true", payload["has_observed_evidence"])
		}
		if payload["has_declared_evidence"] != false || payload["has_applied_evidence"] != false {
			t.Fatalf("declared/applied evidence must stay false for observed-only inventory: %#v", payload)
		}
	}
}

func TestCloudInventoryAdmissionEndToEndAttachesAzureResourceChangeFreshness(t *testing.T) {
	t.Parallel()

	armID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm"
	changeTime := time.Date(2026, time.June, 16, 10, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{
			{facts.AzureCloudResourceFactKind, armID, []byte(`{
				"arm_resource_id":"` + armID + `",
				"resource_type":"microsoft.compute/virtualmachines"
			}`)},
		}},
		{rows: [][]any{
			{
				facts.AzureResourceChangeFactKind,
				armID,
				"change-stable-1",
				[]byte(`{
					"target_arm_resource_id":"` + armID + `",
					"change_type":"updated",
					"change_time":"` + changeTime.Format(time.RFC3339Nano) + `",
					"operation":"Microsoft.Compute/virtualMachines/write",
					"actor_fingerprint":"actor-marker"
				}`),
			},
		}},
	}}
	handler := newCloudInventoryAdmissionHandlerWithFreshness(db)
	intent := cloudInventoryAdmissionIntent()
	intent.ScopeID = "azure:tenant:subscription:sub-1:all:all:resource_graph"
	intent.GenerationID = "gen-inventory-1"

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("loader queries = %d, want %d", got, want)
	}
	changeQuery := db.queries[1]
	if got, want := changeQuery.args[0], intent.ScopeID; got != want {
		t.Fatalf("resource-change query scope arg = %v, want inventory scope %v", got, want)
	}
	if got, want := changeQuery.args[1], intent.GenerationID; got != want {
		t.Fatalf("resource-change query generation arg = %v, want inventory generation %v", got, want)
	}
	if len(db.execs) != 1 {
		t.Fatalf("canonical upserts = %d, want 1", len(db.execs))
	}
	rows := decodeCloudInventoryBatchedRows(t, db.execs)
	if len(rows) != 1 {
		t.Fatalf("canonical rows = %d, want 1", len(rows))
	}
	payload := cloudInventoryDecodePayload(t, rows[0].payload)
	freshness, ok := payload["resource_change_freshness"].([]any)
	if !ok || len(freshness) != 1 {
		t.Fatalf("resource_change_freshness = %#v, want one row", payload["resource_change_freshness"])
	}
	row, ok := freshness[0].(map[string]any)
	if !ok {
		t.Fatalf("freshness row type = %T, want map", freshness[0])
	}
	if got, want := row["change_type"], "updated"; got != want {
		t.Fatalf("change_type = %v, want %v", got, want)
	}
	if _, leaked := row["target_arm_resource_id"]; leaked {
		t.Fatalf("freshness payload leaked raw ARM identity: %#v", row)
	}
}

// TestCloudInventoryAdmissionEndToEndIsIdempotent proves a replayed admission of
// the same generation derives the same canonical fact ids, so the
// ON CONFLICT (fact_id) DO UPDATE upsert converges on one row per uid rather
// than duplicating canonical truth.
func TestCloudInventoryAdmissionEndToEndIsIdempotent(t *testing.T) {
	t.Parallel()

	first := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: cloudInventorySourceRows()}}}
	second := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: cloudInventorySourceRows()}}}

	if _, err := newCloudInventoryAdmissionHandler(first).Handle(context.Background(), cloudInventoryAdmissionIntent()); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	if _, err := newCloudInventoryAdmissionHandler(second).Handle(context.Background(), cloudInventoryAdmissionIntent()); err != nil {
		t.Fatalf("second Handle() error = %v", err)
	}
	firstRows := decodeCloudInventoryBatchedRows(t, first.execs)
	secondRows := decodeCloudInventoryBatchedRows(t, second.execs)
	if len(firstRows) != len(secondRows) {
		t.Fatalf("canonical row count drift: first=%d second=%d", len(firstRows), len(secondRows))
	}
	for i := range firstRows {
		if firstRows[i].factID != secondRows[i].factID {
			t.Fatalf("canonical fact id drifted across replay at row %d: %v != %v", i, firstRows[i].factID, secondRows[i].factID)
		}
	}
}

// TestCloudInventoryAdmissionEndToEndSkipsStaleGeneration proves a superseded
// generation neither loads source facts nor writes canonical rows.
func TestCloudInventoryAdmissionEndToEndSkipsStaleGeneration(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: cloudInventorySourceRows()}}}
	handler := newCloudInventoryAdmissionHandler(db)
	handler.GenerationCheck = func(context.Context, string, string) (bool, error) {
		return false, nil
	}

	result, err := handler.Handle(context.Background(), cloudInventoryAdmissionIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != reducer.ResultStatusSuperseded {
		t.Fatalf("Status = %q, want %q", result.Status, reducer.ResultStatusSuperseded)
	}
	if len(db.queries) != 0 {
		t.Fatalf("stale generation issued %d loader queries, want 0", len(db.queries))
	}
	if len(db.execs) != 0 {
		t.Fatalf("stale generation wrote %d canonical rows, want 0", len(db.execs))
	}
}

// TestCloudInventoryAdmissionEndToEndEmptyGeneration proves an empty generation
// admits no canonical rows and surfaces a clean success rather than fabricating
// identities.
func TestCloudInventoryAdmissionEndToEndEmptyGeneration(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	result, err := newCloudInventoryAdmissionHandler(db).Handle(context.Background(), cloudInventoryAdmissionIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != reducer.ResultStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, reducer.ResultStatusSucceeded)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if len(db.execs) != 0 {
		t.Fatalf("empty generation wrote %d canonical rows, want 0", len(db.execs))
	}
}

// cloudInventoryBatchedRow is one canonical fact row recovered from a batched
// reducer fact insert. The reducer writer now sends all admitted resources as
// parallel arrays in one ExecContext call, so tests decode those arrays back
// into per-row records to make the same fact-id/kind/payload assertions.
type cloudInventoryBatchedRow struct {
	factID   string
	factKind string
	payload  []byte
}

// decodeCloudInventoryBatchedRows flattens every batched canonical fact insert
// recorded by the fake DB into its per-row records. It asserts the batched array
// shape (fact_id, fact_kind, payload arrays), so a regression to per-row inserts
// would surface as a type mismatch here.
func decodeCloudInventoryBatchedRows(t *testing.T, execs []fakeExecCall) []cloudInventoryBatchedRow {
	t.Helper()
	var rows []cloudInventoryBatchedRow
	for _, exec := range execs {
		if len(exec.args) != 15 {
			t.Fatalf("batched insert args = %d, want 15", len(exec.args))
		}
		factIDs, ok := exec.args[0].([]string)
		if !ok {
			t.Fatalf("fact_id arg type = %T, want []string (batched insert)", exec.args[0])
		}
		factKinds, ok := exec.args[3].([]string)
		if !ok {
			t.Fatalf("fact_kind arg type = %T, want []string (batched insert)", exec.args[3])
		}
		payloads, ok := exec.args[14].([]string)
		if !ok {
			t.Fatalf("payload arg type = %T, want []string (batched insert)", exec.args[14])
		}
		for i := range factIDs {
			rows = append(rows, cloudInventoryBatchedRow{
				factID:   factIDs[i],
				factKind: factKinds[i],
				payload:  []byte(payloads[i]),
			})
		}
	}
	return rows
}

func cloudInventoryDecodePayload(t *testing.T, arg any) map[string]any {
	t.Helper()
	raw, ok := arg.([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", arg)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode canonical payload: %v", err)
	}
	return payload
}
