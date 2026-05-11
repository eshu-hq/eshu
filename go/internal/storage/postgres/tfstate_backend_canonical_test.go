package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// fixtureBackendFact builds a backend JSON array compatible with the
// parser-emitted parsed_file_data.terraform_backends bucket shape from
// go/internal/parser/hcl/terraform_backend.go.
func fixtureBackendFact(backendKind, bucket, key, region string) []byte {
	return []byte(`[{
		"backend_kind":"` + backendKind + `",
		"bucket":"` + bucket + `",
		"bucket_is_literal":true,
		"key":"` + key + `",
		"key_is_literal":true,
		"region":"` + region + `",
		"region_is_literal":true
	}]`)
}

// expectedLocatorHash mirrors the collector's canonical hash so tests can
// drive the SUT with the same composite key the resolver receives at runtime.
func expectedLocatorHash(bucket, key string) string {
	return terraformstate.LocatorHash(terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://" + bucket + "/" + key,
	})
}

func TestPostgresTerraformBackendQueryReturnsEmptyOnNoRows(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	query := PostgresTerraformBackendQuery{DB: db}

	rows, err := query.ListTerraformBackendsByLocator(context.Background(), "s3", "any-hash")
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}
	if rows != nil {
		t.Fatalf("rows = %#v, want nil", rows)
	}
}

func TestPostgresTerraformBackendQuerySingleOwnerSingleSnapshot(t *testing.T) {
	t.Parallel()

	bucket := "app-tfstate-prod"
	key := "services/api/terraform.tfstate"
	hash := expectedLocatorHash(bucket, key)
	observed := time.Date(2026, time.May, 11, 14, 0, 0, 0, time.UTC)

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{
			"repo-infra",
			"repository:repo-infra",
			"gen-abc",
			observed,
			fixtureBackendFact("s3", bucket, key, "us-east-1"),
		}}}},
	}
	query := PostgresTerraformBackendQuery{DB: db}

	rows, err := query.ListTerraformBackendsByLocator(context.Background(), "s3", hash)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	got := rows[0]
	want := tfstatebackend.TerraformBackendRow{
		RepoID:           "repo-infra",
		ScopeID:          "repository:repo-infra",
		CommitID:         "gen-abc",
		CommitObservedAt: observed,
		BackendKind:      "s3",
		LocatorHash:      hash,
	}
	if got != want {
		t.Fatalf("row = %#v, want %#v", got, want)
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	for _, fragment := range []string{
		"FROM fact_records",
		"terraform_backends",
		"active_generation_id",
		"generation.status = 'active'",
		"fact.source_system = 'git'",
	} {
		if !strings.Contains(db.queries[0].query, fragment) {
			t.Fatalf("query missing %q: %s", fragment, db.queries[0].query)
		}
	}
}

func TestPostgresTerraformBackendQueryFiltersByCompositeKey(t *testing.T) {
	t.Parallel()

	matchBucket, matchKey := "match-bucket", "envs/prod.tfstate"
	otherBucket, otherKey := "other-bucket", "envs/dev.tfstate"
	hash := expectedLocatorHash(matchBucket, matchKey)
	observed := time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC)

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{
			{
				"repo-match",
				"repository:repo-match",
				"gen-1",
				observed,
				fixtureBackendFact("s3", matchBucket, matchKey, "us-east-1"),
			},
			{
				"repo-other",
				"repository:repo-other",
				"gen-2",
				observed,
				fixtureBackendFact("s3", otherBucket, otherKey, "us-east-1"),
			},
		}}},
	}
	query := PostgresTerraformBackendQuery{DB: db}

	rows, err := query.ListTerraformBackendsByLocator(context.Background(), "s3", hash)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0].RepoID, "repo-match"; got != want {
		t.Fatalf("rows[0].RepoID = %q, want %q", got, want)
	}
}

func TestPostgresTerraformBackendQuerySurfacesAmbiguousOwners(t *testing.T) {
	t.Parallel()

	// Two distinct repos claim the same (backend_kind, locator_hash). The
	// adapter MUST return both rows so the resolver can detect ambiguity and
	// emit ErrAmbiguousBackendOwner.
	bucket, key := "shared-state", "envs/prod.tfstate"
	hash := expectedLocatorHash(bucket, key)
	observed := time.Date(2026, time.May, 10, 9, 0, 0, 0, time.UTC)

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{
			{
				"repo-a",
				"repository:repo-a",
				"gen-a1",
				observed,
				fixtureBackendFact("s3", bucket, key, "us-east-1"),
			},
			{
				"repo-b",
				"repository:repo-b",
				"gen-b1",
				observed,
				fixtureBackendFact("s3", bucket, key, "us-east-1"),
			},
		}}},
	}
	query := PostgresTerraformBackendQuery{DB: db}

	rows, err := query.ListTerraformBackendsByLocator(context.Background(), "s3", hash)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d (adapter must not pre-filter ambiguity)", got, want)
	}
	seen := map[string]struct{}{}
	for _, row := range rows {
		seen[row.RepoID] = struct{}{}
	}
	for _, repoID := range []string{"repo-a", "repo-b"} {
		if _, ok := seen[repoID]; !ok {
			t.Fatalf("expected repo %q in result, got %#v", repoID, rows)
		}
	}
}

func TestPostgresTerraformBackendQueryReturnsMultipleSnapshotsForSingleOwner(t *testing.T) {
	t.Parallel()

	// Same repo, two sealed snapshots, both carrying the same backend block.
	// The adapter returns both; the resolver picks the latest by CommitObservedAt.
	bucket, key := "single-owner", "envs/prod.tfstate"
	hash := expectedLocatorHash(bucket, key)
	earlier := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC)

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{
			{
				"repo-mono",
				"repository:repo-mono",
				"gen-old",
				earlier,
				fixtureBackendFact("s3", bucket, key, "us-east-1"),
			},
			{
				"repo-mono",
				"repository:repo-mono",
				"gen-new",
				later,
				fixtureBackendFact("s3", bucket, key, "us-east-1"),
			},
		}}},
	}
	query := PostgresTerraformBackendQuery{DB: db}

	rows, err := query.ListTerraformBackendsByLocator(context.Background(), "s3", hash)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	commitIDs := []string{rows[0].CommitID, rows[1].CommitID}
	wantSet := map[string]struct{}{"gen-old": {}, "gen-new": {}}
	for _, id := range commitIDs {
		if _, ok := wantSet[id]; !ok {
			t.Fatalf("unexpected CommitID %q", id)
		}
	}
}

func TestPostgresTerraformBackendQueryRejectsBlankInputs(t *testing.T) {
	t.Parallel()

	query := PostgresTerraformBackendQuery{DB: &fakeExecQueryer{}}

	if _, err := query.ListTerraformBackendsByLocator(context.Background(), "", "hash"); err == nil {
		t.Fatalf("blank backend kind: error = nil, want non-nil")
	}
	if _, err := query.ListTerraformBackendsByLocator(context.Background(), "s3", ""); err == nil {
		t.Fatalf("blank locator hash: error = nil, want non-nil")
	}
}

func TestPostgresTerraformBackendQueryRequiresDatabase(t *testing.T) {
	t.Parallel()

	var query PostgresTerraformBackendQuery
	_, err := query.ListTerraformBackendsByLocator(context.Background(), "s3", "hash")
	if err == nil {
		t.Fatalf("nil DB: error = nil, want non-nil")
	}
}

func TestPostgresTerraformBackendQueryPropagatesQueryError(t *testing.T) {
	t.Parallel()

	want := errors.New("boom")
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{err: want}}}
	query := PostgresTerraformBackendQuery{DB: db}

	_, err := query.ListTerraformBackendsByLocator(context.Background(), "s3", "hash")
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want errors.Is(_, want)=true", err)
	}
}
