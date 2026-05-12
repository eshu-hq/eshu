package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/scope"
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

// expectedLocatorHash mirrors the canonical resolver join hash so tests can
// drive the SUT with the same composite key the resolver receives at runtime.
// This MUST stay scope-aligned (see ScopeLocatorHash and issue #203).
func expectedLocatorHash(bucket, key string) string {
	return terraformstate.ScopeLocatorHash(
		terraformstate.BackendS3,
		"s3://"+bucket+"/"+key,
	)
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

// TestPostgresTerraformBackendQueryMatchesScopeIDDerivedHash is the
// regression guard for issue #203. The drift handler parses the locator hash
// out of a state-snapshot scope ID (built by
// scope.NewTerraformStateSnapshotScope, which uses hashStateLocator) and
// hands it to the resolver, which calls this adapter. Before the fix the
// adapter recomputed candidate hashes with terraformstate.LocatorHash and
// the two hashes diverged for empty VersionID by exactly one trailing null
// byte, silently rejecting every drift candidate with
// ErrNoConfigRepoOwnsBackend. This test wires the real production hash path
// end-to-end (scope ID -> parsed hash -> adapter lookup) so the divergence
// cannot regress.
func TestPostgresTerraformBackendQueryMatchesScopeIDDerivedHash(t *testing.T) {
	t.Parallel()

	bucket := "eshu-drift-b"
	key := "prod/terraform.tfstate"
	locator := "s3://" + bucket + "/" + key

	// Build the scope ID exactly as the production state-side path does.
	scopeValue, err := scope.NewTerraformStateSnapshotScope(
		"repo-scope-test",
		string(terraformstate.BackendS3),
		locator,
		nil,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	scopeHash := strings.TrimPrefix(
		scopeValue.ScopeID,
		"state_snapshot:"+string(terraformstate.BackendS3)+":",
	)

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

	rows, err := query.ListTerraformBackendsByLocator(
		context.Background(),
		string(terraformstate.BackendS3),
		scopeHash,
	)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d; the canonical join hash diverged from "+
			"the state-snapshot scope hash. See issue #203.", got, want)
	}
	if got, want := rows[0].LocatorHash, scopeHash; got != want {
		t.Fatalf("rows[0].LocatorHash = %q, want %q (scope-derived hash)", got, want)
	}
	if got, want := rows[0].RepoID, "repo-infra"; got != want {
		t.Fatalf("rows[0].RepoID = %q, want %q", got, want)
	}
}

// TestPostgresTerraformBackendQueryAmbiguousScopeIDDerivedHash exercises the
// ambiguous-owner path through the same scope-derived hash to round out the
// positive/negative/ambiguous proof matrix for issue #203. The state-side
// scope ID derives one hash; two distinct repo facts both contain a
// terraform_backends row that hashes to the same value; the adapter MUST
// return both rows so the resolver can emit ErrAmbiguousBackendOwner.
func TestPostgresTerraformBackendQueryAmbiguousScopeIDDerivedHash(t *testing.T) {
	t.Parallel()

	bucket := "shared-tfstate"
	key := "envs/prod/terraform.tfstate"
	locator := "s3://" + bucket + "/" + key

	scopeValue, err := scope.NewTerraformStateSnapshotScope(
		"repo-scope-test",
		string(terraformstate.BackendS3),
		locator,
		nil,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	scopeHash := strings.TrimPrefix(
		scopeValue.ScopeID,
		"state_snapshot:"+string(terraformstate.BackendS3)+":",
	)

	observed := time.Date(2026, time.May, 11, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{
			{
				"repo-team-a",
				"repository:repo-team-a",
				"gen-team-a",
				observed,
				fixtureBackendFact("s3", bucket, key, "us-east-1"),
			},
			{
				"repo-team-b",
				"repository:repo-team-b",
				"gen-team-b",
				observed,
				fixtureBackendFact("s3", bucket, key, "us-east-1"),
			},
		}}},
	}
	query := PostgresTerraformBackendQuery{DB: db}

	rows, err := query.ListTerraformBackendsByLocator(
		context.Background(),
		string(terraformstate.BackendS3),
		scopeHash,
	)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d; ambiguous owners must both surface so "+
			"the resolver can emit ErrAmbiguousBackendOwner", got, want)
	}
	repoIDs := map[string]struct{}{rows[0].RepoID: {}, rows[1].RepoID: {}}
	for _, want := range []string{"repo-team-a", "repo-team-b"} {
		if _, ok := repoIDs[want]; !ok {
			t.Fatalf("rows missing repo %q; got %#v", want, repoIDs)
		}
	}
}

// TestPostgresTerraformBackendQueryRejectsLocatorHashWithVersionDigest is the
// negative-case guard for issue #203. If a caller mistakenly hands the
// adapter a per-version LocatorHash (the old, buggy production behavior),
// the lookup must NOT match a scope-aligned config-side row. This pins the
// observable behavior: the version-aware hash is a different hash space and
// cannot be joined against the scope hash.
func TestPostgresTerraformBackendQueryRejectsLocatorHashWithVersionDigest(t *testing.T) {
	t.Parallel()

	bucket := "eshu-drift-b"
	key := "prod/terraform.tfstate"
	locator := "s3://" + bucket + "/" + key

	// The buggy hash a pre-fix call site would produce: LocatorHash with empty
	// VersionID, which differs from ScopeLocatorHash by one trailing null byte.
	buggyHash := terraformstate.LocatorHash(terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     locator,
	})
	scopeAlignedHash := terraformstate.ScopeLocatorHash(terraformstate.BackendS3, locator)
	if buggyHash == scopeAlignedHash {
		t.Fatalf("LocatorHash and ScopeLocatorHash must differ for empty VersionID; "+
			"this test cannot exercise the bug if they agree. got %q == %q",
			buggyHash, scopeAlignedHash)
	}

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

	rows, err := query.ListTerraformBackendsByLocator(
		context.Background(),
		string(terraformstate.BackendS3),
		buggyHash,
	)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0; lookup with the per-version LocatorHash "+
			"must not match a scope-aligned config-side row", len(rows))
	}
}
