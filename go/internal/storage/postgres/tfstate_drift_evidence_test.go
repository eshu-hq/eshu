package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// fixtureConfigParserRow mirrors the parser's parsed_file_data.terraform_resources
// row shape from go/internal/parser/hcl/parser.go:130-154. The loader
// reconstructs the canonical address from resource_type + resource_name (the
// parser does not emit a single "address" key on the config side).
func fixtureConfigParserRow(resourceType, resourceName string) string {
	return `{
		"name":"` + resourceType + `.` + resourceName + `",
		"resource_type":"` + resourceType + `",
		"resource_name":"` + resourceName + `",
		"path":"main.tf",
		"lang":"hcl",
		"line_number":1
	}`
}

// fixtureConfigResourcesArray wraps one or more parser rows into the JSON
// array shape stored at parsed_file_data.terraform_resources.
func fixtureConfigResourcesArray(rows ...string) []byte {
	out := "["
	for i, row := range rows {
		if i > 0 {
			out += ","
		}
		out += row
	}
	return []byte(out + "]")
}

// fixtureStatePayload returns the JSON shape the collector emits for one
// terraform_state_resource fact, mirroring
// go/internal/collector/terraformstate/resources.go:173-181.
func fixtureStatePayload(address, resourceType, name, attributesJSON string) []byte {
	if attributesJSON == "" {
		attributesJSON = "{}"
	}
	return []byte(`{
		"address":"` + address + `",
		"mode":"managed",
		"type":"` + resourceType + `",
		"name":"` + name + `",
		"module":"",
		"attributes":` + attributesJSON + `
	}`)
}

// fixtureSnapshotRow returns the snapshot-metadata row shape:
// (lineage, serial, generation_id).
func fixtureSnapshotRow(lineage string, serial int64, generationID string) []any {
	return []any{lineage, serial, generationID}
}

// fixtureStateResourceRow returns a row of the state-resource query in the
// shape (address, payload_json).
func fixtureStateResourceRow(address string, payload []byte) []any {
	return []any{address, payload}
}

func TestPostgresDriftEvidenceLoaderConfigOnlyAddress(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a1",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. config-side query: one terraform_resources file with one entry.
			{rows: [][]any{{
				fixtureConfigResourcesArray(fixtureConfigParserRow("aws_iam_role", "svc")),
			}}},
			// 2. current snapshot lookup: serial=0 (no prior possible).
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 0, "gen-state-current")}},
			// 3. current state-resource rows: none.
			{rows: [][]any{}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]
	if got, want := row.Address, "aws_iam_role.svc"; got != want {
		t.Fatalf("row.Address = %q, want %q", got, want)
	}
	if row.Config == nil {
		t.Fatalf("row.Config = nil, want non-nil")
	}
	if got, want := row.Config.ResourceType, "aws_iam_role"; got != want {
		t.Fatalf("row.Config.ResourceType = %q, want %q", got, want)
	}
	if row.State != nil {
		t.Fatalf("row.State = %#v, want nil", row.State)
	}
	if row.Prior != nil {
		t.Fatalf("row.Prior = %#v, want nil", row.Prior)
	}

	// Serial=0 short-circuit: only 3 queries must run (no prior lookup).
	if got, want := len(db.queries), 3; got != want {
		t.Fatalf("query count = %d, want %d (serial=0 must skip prior)", got, want)
	}
}

func TestPostgresDriftEvidenceLoaderStateOnlyAddress(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a1",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// config-side: empty.
			{rows: [][]any{}},
			// snapshot: serial=0 (no prior).
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 0, "gen-state-current")}},
			// state-resource: one entry, no config counterpart.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_s3_bucket.logs",
				fixtureStatePayload("aws_s3_bucket.logs", "aws_s3_bucket", "logs", `{}`),
			)}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if rows[0].Config != nil {
		t.Fatalf("row.Config = %#v, want nil", rows[0].Config)
	}
	if rows[0].State == nil {
		t.Fatalf("row.State = nil, want non-nil")
	}
	if got, want := rows[0].State.ResourceType, "aws_s3_bucket"; got != want {
		t.Fatalf("row.State.ResourceType = %q, want %q", got, want)
	}
}

func TestPostgresDriftEvidenceLoaderPriorGenerationFetched(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a1",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// config-side: lambda still declared.
			{rows: [][]any{{
				fixtureConfigResourcesArray(fixtureConfigParserRow("aws_lambda_function", "worker")),
			}}},
			// snapshot: serial=5 (prior possible).
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			// current state-resource: lambda removed from state.
			{rows: [][]any{}},
			// prior snapshot lookup: serial=4 of same lineage.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			// prior state-resource: lambda was present.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_lambda_function.worker",
				fixtureStatePayload("aws_lambda_function.worker", "aws_lambda_function", "worker", `{}`),
			)}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]
	if row.Prior == nil {
		t.Fatalf("row.Prior = nil, want non-nil")
	}
	if row.Prior.LineageRotation {
		t.Fatalf("row.Prior.LineageRotation = true, want false (same lineage)")
	}

	// Five queries total: config + snapshot + current-state + prior-snapshot + prior-state.
	if got, want := len(db.queries), 5; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
}

func TestPostgresDriftEvidenceLoaderLineageRotationFlagged(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a1",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				fixtureConfigResourcesArray(fixtureConfigParserRow("aws_lambda_function", "worker")),
			}}},
			// snapshot: lineage-2 current.
			{rows: [][]any{fixtureSnapshotRow("lineage-2", 5, "gen-state-current")}},
			{rows: [][]any{}},
			// prior snapshot: DIFFERENT lineage -> rotation.
			{rows: [][]any{fixtureSnapshotRow("lineage-1-old", 4, "gen-state-prior")}},
			{rows: [][]any{fixtureStateResourceRow(
				"aws_lambda_function.worker",
				fixtureStatePayload("aws_lambda_function.worker", "aws_lambda_function", "worker", `{}`),
			)}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if rows[0].Prior == nil {
		t.Fatalf("row.Prior = nil, want non-nil")
	}
	if !rows[0].Prior.LineageRotation {
		t.Fatalf("row.Prior.LineageRotation = false, want true (lineage mismatch)")
	}
}

func TestPostgresDriftEvidenceLoaderStateOnlyWithPriorLeavesFlagFalse(t *testing.T) {
	t.Parallel()

	// State has an address that the prior generation also had (e.g. a
	// resource that was operator-imported into state and persists across
	// generations). v1 deliberately leaves PreviouslyDeclaredInConfig=false
	// so the classifier emits added_in_state — the safer fallback for cases
	// where we cannot cheaply prove the address was once in config.
	// Setting the flag from prior-state existence (the previous behavior)
	// would misclassify operator-imported resources as removed_from_config.
	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a1",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// config-side: empty.
			{rows: [][]any{}},
			// current snapshot: serial=5.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			// current state-resource: address present in state but not config.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_role.imported",
				fixtureStatePayload("aws_iam_role.imported", "aws_iam_role", "imported", `{}`),
			)}},
			// prior snapshot: same lineage, serial=4.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			// prior state-resource: same address (persisted across generations).
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_role.imported",
				fixtureStatePayload("aws_iam_role.imported", "aws_iam_role", "imported", `{}`),
			)}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if rows[0].State == nil {
		t.Fatalf("row.State = nil, want non-nil")
	}
	if rows[0].State.PreviouslyDeclaredInConfig {
		t.Fatalf("row.State.PreviouslyDeclaredInConfig = true, want false (v1 conservative fallback; see mergeDriftRows comment)")
	}
}

func TestPostgresDriftEvidenceLoaderNoSnapshotReturnsEmpty(t *testing.T) {
	t.Parallel()

	// A state-snapshot scope without an active terraform_state_snapshot fact
	// has no usable lineage/serial; the loader cannot pair config and state
	// rows by generation. Return an empty slice (operator-actionable case for
	// the reducer to log; never an error).
	anchor := tfstatebackend.CommitAnchor{
		RepoID: "repo-a", ScopeID: "repository:repo-a", CommitID: "gen-a1",
	}
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
			{rows: [][]any{}}, // no snapshot row
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadDriftEvidence(context.Background(), "state_snapshot:s3:hash", anchor)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if rows != nil {
		t.Fatalf("rows = %#v, want nil", rows)
	}
}

func TestPostgresDriftEvidenceLoaderRequiresDatabase(t *testing.T) {
	t.Parallel()

	var loader PostgresDriftEvidenceLoader
	_, err := loader.LoadDriftEvidence(
		context.Background(),
		"state_snapshot:s3:hash",
		tfstatebackend.CommitAnchor{ScopeID: "repository:r", CommitID: "g"},
	)
	if err == nil {
		t.Fatalf("nil DB: error = nil, want non-nil")
	}
}

// Compile-time guard that PostgresDriftEvidenceLoader satisfies the reducer's
// DriftEvidenceLoader interface. If this fails to compile, the adapter is not
// usable as a DefaultHandlers.DriftEvidenceLoader.
var _ interface {
	LoadDriftEvidence(
		ctx context.Context,
		stateScopeID string,
		anchor tfstatebackend.CommitAnchor,
	) ([]tfconfigstate.AddressedRow, error)
} = PostgresDriftEvidenceLoader{}
