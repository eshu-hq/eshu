package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// fixturePriorConfigAddressesArray wraps one or more parser rows into the JSON
// array shape returned by listPriorConfigAddressesQuery. The query SELECTs the
// terraform_resources array per file, so each row in the result is one such
// array. This helper mirrors fixtureConfigResourcesArray for the prior-gen
// shape.
func fixturePriorConfigAddressesArray(rows ...string) []byte {
	return fixtureConfigResourcesArray(rows...)
}

func TestPostgresDriftEvidenceLoaderPriorConfigDeclarationActivatesRemovedFromConfig(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a2",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules walk (#169): no module calls in this fixture.
			{rows: [][]any{}},
			// 2. config-side (current gen): empty.
			{rows: [][]any{}},
			// 3. current snapshot: serial=5.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			// 4. current state-resource: an address that was once declared.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_policy.legacy",
				fixtureStatePayload("aws_iam_policy.legacy", "aws_iam_policy", "legacy", `{}`),
			)}},
			// 5. prior state snapshot lookup: same lineage, serial=4.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			// 6. prior state-resource: same address.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_policy.legacy",
				fixtureStatePayload("aws_iam_policy.legacy", "aws_iam_policy", "legacy", `{}`),
			)}},
			// 7. prior-config addresses walk. The prior gen DID declare this address.
			{rows: [][]any{{
				fixturePriorConfigAddressesArray(fixtureConfigParserRow("aws_iam_policy", "legacy")),
			}}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db, PriorConfigDepth: 10}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if rows[0].State == nil {
		t.Fatalf("row.State = nil, want non-nil")
	}
	if !rows[0].State.PreviouslyDeclaredInConfig {
		t.Fatalf("row.State.PreviouslyDeclaredInConfig = false, want true (prior gen declared this address)")
	}
}

func TestPostgresDriftEvidenceLoaderPriorConfigNeverDeclaredLeavesFlagFalse(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a2",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules walk (#169): no module calls in this fixture.
			{rows: [][]any{}},
			// 2. config-side: empty.
			{rows: [][]any{}},
			// 3. current snapshot: serial=5.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			// 4. current state-resource: an operator-imported address.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_role.imported",
				fixtureStatePayload("aws_iam_role.imported", "aws_iam_role", "imported", `{}`),
			)}},
			// 5. prior snapshot: same lineage.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			// 6. prior state-resource: same address persisted.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_role.imported",
				fixtureStatePayload("aws_iam_role.imported", "aws_iam_role", "imported", `{}`),
			)}},
			// 7. prior-config walk: returns NO rows (address never declared anywhere).
			{rows: [][]any{}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db, PriorConfigDepth: 10}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if rows[0].State == nil {
		t.Fatalf("row.State = nil, want non-nil")
	}
	if rows[0].State.PreviouslyDeclaredInConfig {
		t.Fatalf("row.State.PreviouslyDeclaredInConfig = true, want false (no prior gen declared this — operator-imported resource)")
	}
}

func TestPostgresDriftEvidenceLoaderPriorConfigOutsideDepthWindowLeavesFlagFalse(t *testing.T) {
	t.Parallel()

	// The address was declared N+1 generations ago. With PriorConfigDepth=1
	// the walker only sees the most recent prior generation, which does NOT
	// declare the address. PreviouslyDeclaredInConfig stays false; the
	// classifier emits added_in_state — the conservative outside-window
	// fallback documented in the spec.
	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a2",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// terraform_modules walk (#169): no module calls in this fixture.
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_policy.deep_history",
				fixtureStatePayload("aws_iam_policy.deep_history", "aws_iam_policy", "deep_history", `{}`),
			)}},
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_policy.deep_history",
				fixtureStatePayload("aws_iam_policy.deep_history", "aws_iam_policy", "deep_history", `{}`),
			)}},
			// prior-config walk with depth=1: most recent prior gen has no entry for this address.
			{rows: [][]any{{
				fixturePriorConfigAddressesArray(fixtureConfigParserRow("aws_iam_role", "unrelated")),
			}}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db, PriorConfigDepth: 1}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if rows[0].State == nil {
		t.Fatalf("row.State = nil, want non-nil")
	}
	if rows[0].State.PreviouslyDeclaredInConfig {
		t.Fatalf("row.State.PreviouslyDeclaredInConfig = true, want false (address only in N+1 deeper, outside depth window)")
	}
}
