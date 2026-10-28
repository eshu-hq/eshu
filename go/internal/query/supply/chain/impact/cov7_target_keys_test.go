// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type cov7TargetQueryer struct {
	onQuery func(query string) (db.Rows, error)
}

func (q *cov7TargetQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	return q.onQuery(query)
}

type cov7TargetRows struct {
	rows     [][]any
	pos      int
	scanErr  error
	iterErr  error
	closeErr error
}

func (r *cov7TargetRows) Next() bool { return r.pos < len(r.rows) }

func (r *cov7TargetRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	row := r.rows[r.pos]
	r.pos++
	for i, value := range row {
		switch target := dest[i].(type) {
		case *string:
			*target = value.(string)
		case *bool:
			*target = value.(bool)
		default:
			return errors.New("unexpected scan target")
		}
	}
	return nil
}

func (r *cov7TargetRows) Err() error { return r.iterErr }

func (r *cov7TargetRows) Close() error { return r.closeErr }

func cov7TargetKey(packageID, ecosystem, name string, trusted bool) readinessTargetKey {
	return readinessTargetKey{
		packageID:     packageID,
		key:           packageidentity.ConsumptionKey{Ecosystem: packageidentity.Ecosystem(ecosystem), PackageName: name},
		parserTrusted: trusted,
	}
}

// TestCov7ReadinessTargetArguments proves the ecosystem/name/package
// projection and the owner-argument trusted split.
func TestCov7ReadinessTargetArguments(t *testing.T) {
	t.Parallel()

	target := readinessTarget{
		PackageIDs: []string{"p1"},
		Keys: []readinessTargetKey{
			cov7TargetKey("p1", "npm", "lodash", true),
			cov7TargetKey("p1", "npm", "lodash", false),
		},
	}
	ecosystems, names, ids, _ := readinessTargetArguments(target, ReadinessQuery{})
	if len(ecosystems) != 2 || ecosystems[0] != "npm" || names[1] != "lodash" || len(ids) != 1 {
		t.Fatalf("arguments = (%v, %v, %v), want projected slices", ecosystems, names, ids)
	}
	packageIDs, _, _, trustedIDs, trustedEcos, trustedNames := readinessTargetOwnerArguments(target)
	if len(packageIDs) != 2 || len(trustedIDs) != 1 || trustedEcos[0] != "npm" || trustedNames[0] != "lodash" {
		t.Fatalf("owner arguments split wrong: all %d trusted %d", len(packageIDs), len(trustedIDs))
	}
}

// TestCov7ResolveReadinessTargetPackage proves the PURL, default-registry,
// and package-ID resolution branches with their fail-closed errors.
func TestCov7ResolveReadinessTargetPackage(t *testing.T) {
	t.Parallel()

	resolved, err := resolveReadinessTargetPackage("npm://registry.npmjs.org/lodash", "pkg:npm/lodash@4.17.21", "", "", nil)
	if err != nil || resolved.packageID != "npm://registry.npmjs.org/lodash" || len(resolved.keys) != 1 || !resolved.keys[0].parserTrusted {
		t.Fatalf("purl resolve = (%#v, %v), want trusted lodash identity", resolved, err)
	}
	if _, err := resolveReadinessTargetPackage("npm://registry.npmjs.org/other", "pkg:npm/lodash@4.17.21", "", "", nil); err == nil {
		t.Fatalf("disagreeing package ID = nil error, want identity mismatch")
	}
	if _, err := resolveReadinessTargetPackage("", "bogus", "", "", nil); err == nil {
		t.Fatalf("keyless purl = nil error, want no-identity error")
	}
	if _, err := resolveReadinessTargetPackage("", "pkg:unknown/x@1", "", "", nil); err == nil {
		t.Fatalf("bad-ecosystem purl = nil error, want parse error")
	}
	registry, err := resolveReadinessTargetPackage("", "", "npm", "lodash", nil)
	if err != nil || registry.packageID != "npm://registry.npmjs.org/lodash" {
		t.Fatalf("default-registry resolve = (%#v, %v), want lodash identity", registry, err)
	}
	if _, err := resolveReadinessTargetPackage("", "", "boguseco", "x", nil); err == nil {
		t.Fatalf("unknown ecosystem = nil error, want identity error")
	}
	fallback, err := resolveReadinessTargetPackage("npm://registry.npmjs.org/lodash", "", "", "", map[string][]packageidentity.ConsumptionKey{
		"npm://registry.npmjs.org/lodash": {{Ecosystem: "npm", PackageName: "lodash"}},
	})
	if err != nil || len(fallback.keys) == 0 {
		t.Fatalf("package-id fallback = (%#v, %v), want merged keys", fallback, err)
	}
	if _, err := resolveReadinessTargetPackage("bogus-id", "", "", "", nil); err == nil {
		t.Fatalf("keyless package ID = nil error, want no-identity error")
	}
}

// TestCov7ReadinessTargetSetAlgebra proves intersection, merging, key
// construction filtering, and trust-OR dedup with sorting.
func TestCov7ReadinessTargetSetAlgebra(t *testing.T) {
	t.Parallel()

	if got := intersectReadinessTargetPackages(nil); len(got.PackageIDs) != 0 || len(got.Keys) != 0 {
		t.Fatalf("empty intersection = %#v, want empty", got)
	}
	mkPkg := func(keys ...readinessTargetKey) readinessTargetPackage {
		return readinessTargetPackage{packageID: "p", keys: keys}
	}
	intersected := intersectReadinessTargetPackages([]map[string]readinessTargetPackage{
		{"p": mkPkg(cov7TargetKey("p", "npm", "a", false)), "q": mkPkg()},
		{"p": mkPkg(cov7TargetKey("p", "npm", "b", true))},
	})
	if len(intersected.PackageIDs) != 1 || intersected.PackageIDs[0] != "p" {
		t.Fatalf("intersection = %#v, want [p]", intersected.PackageIDs)
	}
	if len(intersected.Keys) != 2 {
		t.Fatalf("intersection keys = %#v, want union across sets", intersected.Keys)
	}
	merged := mergeReadinessTargetPackage(readinessTargetPackage{}, mkPkg(cov7TargetKey("p", "npm", "a", false)))
	if merged.packageID != "p" {
		t.Fatalf("empty merge = %#v, want candidate", merged)
	}
	fromKeys := readinessTargetPackageFromKeys("p", []packageidentity.ConsumptionKey{
		{Ecosystem: "npm", PackageName: "a"},
		{Ecosystem: "", PackageName: "b"},
		{Ecosystem: "npm", PackageName: ""},
	}, true)
	if len(fromKeys.keys) != 1 || !fromKeys.keys[0].parserTrusted {
		t.Fatalf("fromKeys = %#v, want one trusted key", fromKeys)
	}
	unique := uniqueReadinessTargetKeys([]readinessTargetKey{
		cov7TargetKey("p", "npm", "b", false),
		cov7TargetKey("", "npm", "b", true),
		cov7TargetKey("p", "npm", "", true),
		cov7TargetKey("p", "npm", "a", false),
		cov7TargetKey("p", "npm", "a", true),
	})
	if len(unique) != 2 || unique[0].key.PackageName != "a" || !unique[0].parserTrusted || unique[1].key.PackageName != "b" {
		t.Fatalf("unique = %#v, want sorted trust-OR pair", unique)
	}
	if _, err := readinessTargetPackageForPackageID("  ", nil); err == nil {
		t.Fatalf("blank package ID = nil error, want identity error")
	}
}

// TestCov7ResolveReadinessImageDigests proves direct digests bypass the
// reader while image references resolve through it with trim, direct-match
// filtering, and sorted dedup.
func TestCov7ResolveReadinessImageDigests(t *testing.T) {
	t.Parallel()

	if digests, err := resolveReadinessImageDigests(t.Context(), nil, ReadinessQuery{}); digests != nil || err != nil {
		t.Fatalf("empty query = (%v, %v), want nil nil", digests, err)
	}
	if digests, err := resolveReadinessImageDigests(t.Context(), nil, ReadinessQuery{SubjectDigest: " sha256:abc "}); err != nil || len(digests) != 1 || digests[0] != "sha256:abc" {
		t.Fatalf("direct digest = (%v, %v), want trimmed singleton", digests, err)
	}
	rows := &cov7TargetRows{rows: [][]any{{"sha256:b"}, {"  "}, {"sha256:a"}, {"sha256:b"}}}
	reader := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) { return rows, nil }}
	digests, err := resolveReadinessImageDigests(t.Context(), reader, ReadinessQuery{ImageRef: "img"})
	if err != nil || len(digests) != 2 || digests[0] != "sha256:a" {
		t.Fatalf("image digests = (%v, %v), want sorted deduped pair", digests, err)
	}
	directFiltered, err := resolveReadinessImageDigests(t.Context(),
		&cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
			return &cov7TargetRows{rows: [][]any{{"sha256:a"}, {"sha256:b"}}}, nil
		}},
		ReadinessQuery{ImageRef: "img", SubjectDigest: "sha256:a"})
	if err != nil || len(directFiltered) != 1 || directFiltered[0] != "sha256:a" {
		t.Fatalf("direct-filtered digests = (%v, %v), want only the direct digest", directFiltered, err)
	}
	want := errors.New("reader down")
	failing := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) { return nil, want }}
	if _, err := resolveReadinessImageDigests(t.Context(), failing, ReadinessQuery{ImageRef: "img"}); !errors.Is(err, want) {
		t.Fatalf("query error = %v, want %v", err, want)
	}
	scanFail := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{"x"}}, scanErr: errors.New("scan boom")}, nil
	}}
	if _, err := resolveReadinessImageDigests(t.Context(), scanFail, ReadinessQuery{ImageRef: "img"}); err == nil {
		t.Fatalf("scan failure = nil error, want failure")
	}
	iterFail := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{iterErr: errors.New("iter boom")}, nil
	}}
	if _, err := resolveReadinessImageDigests(t.Context(), iterFail, ReadinessQuery{ImageRef: "img"}); err == nil {
		t.Fatalf("iteration failure = nil error, want failure")
	}
}

// TestCov7PackageManifestConsumptionKeysReady proves the marker probe: query
// failures fail closed, an absent row reports not-ready, and a true row
// reports ready.
func TestCov7PackageManifestConsumptionKeysReady(t *testing.T) {
	t.Parallel()

	want := errors.New("reader down")
	failing := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) { return nil, want }}
	if _, err := packageManifestConsumptionKeysReady(t.Context(), failing); !errors.Is(err, want) {
		t.Fatalf("query error = %v, want %v", err, want)
	}
	absent := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) { return &cov7TargetRows{}, nil }}
	ready, err := packageManifestConsumptionKeysReady(t.Context(), absent)
	if err != nil || ready {
		t.Fatalf("absent marker = (%v, %v), want false nil", ready, err)
	}
	scanFail := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{true}}, scanErr: errors.New("scan boom")}, nil
	}}
	if _, err := packageManifestConsumptionKeysReady(t.Context(), scanFail); err == nil {
		t.Fatalf("scan failure = nil error, want failure")
	}
	iterFail := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{true}}, iterErr: errors.New("iter boom")}, nil
	}}
	// The row scans before the iteration error surfaces, so the probe still
	// reports the scanned value with the iteration error attached.
	if _, err := packageManifestConsumptionKeysReady(t.Context(), iterFail); err == nil {
		t.Fatalf("iteration failure = nil error, want failure")
	}
	readyReader := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{true}}}, nil
	}}
	ready, err = packageManifestConsumptionKeysReady(t.Context(), readyReader)
	if err != nil || !ready {
		t.Fatalf("true marker = (%v, %v), want true nil", ready, err)
	}
}

// TestCov7ValidateReadinessTargetKeyOwners proves the empty-target fast path
// and the ownership verdict mapping through the guarded reader.
func TestCov7ValidateReadinessTargetKeyOwners(t *testing.T) {
	t.Parallel()

	if err := validateReadinessTargetKeyOwners(t.Context(), nil, readinessTarget{}); err != nil {
		t.Fatalf("empty target = %v, want nil", err)
	}
	target := readinessTarget{Keys: []readinessTargetKey{cov7TargetKey("p", "npm", "a", true)}}
	want := errors.New("reader down")
	failing := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) { return nil, want }}
	if err := validateReadinessTargetKeyOwners(t.Context(), failing, target); !errors.Is(err, want) {
		t.Fatalf("query error = %v, want %v", err, want)
	}
	absent := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) { return &cov7TargetRows{}, nil }}
	if err := validateReadinessTargetKeyOwners(t.Context(), absent, target); err != nil {
		t.Fatalf("absent row = %v, want nil", err)
	}
	scanFail := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{true}}, scanErr: errors.New("scan boom")}, nil
	}}
	if err := validateReadinessTargetKeyOwners(t.Context(), scanFail, target); err == nil {
		t.Fatalf("scan failure = nil error, want failure")
	}
	iterFail := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{true}}, iterErr: errors.New("iter boom")}, nil
	}}
	if err := validateReadinessTargetKeyOwners(t.Context(), iterFail, target); err == nil {
		t.Fatalf("iteration failure = nil error, want failure")
	}
	ambiguous := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{false}}}, nil
	}}
	if err := validateReadinessTargetKeyOwners(t.Context(), ambiguous, target); err == nil {
		t.Fatalf("false verdict = nil error, want ambiguity error")
	}
	valid := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{true}}}, nil
	}}
	if err := validateReadinessTargetKeyOwners(t.Context(), valid, target); err != nil {
		t.Fatalf("true verdict = %v, want nil", err)
	}
}

// TestCov7LoadReadinessRegistryKeys proves the registry-identity load: empty
// input short-circuits, failures propagate, and rows convert to consumption
// keys.
func TestCov7LoadReadinessRegistryKeys(t *testing.T) {
	t.Parallel()

	keys, err := loadReadinessRegistryKeysForPackageIDs(t.Context(), nil, nil)
	if err != nil || len(keys) != 0 {
		t.Fatalf("empty load = (%v, %v), want empty map", keys, err)
	}
	want := errors.New("reader down")
	failing := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) { return nil, want }}
	if _, err := loadReadinessRegistryKeysForPackageIDs(t.Context(), failing, []string{"p"}); !errors.Is(err, want) {
		t.Fatalf("query error = %v, want %v", err, want)
	}
	scanFail := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{"p", "npm", "lodash", "lodash", ""}}, scanErr: errors.New("scan boom")}, nil
	}}
	if _, err := loadReadinessRegistryKeysForPackageIDs(t.Context(), scanFail, []string{"p"}); err == nil {
		t.Fatalf("scan failure = nil error, want failure")
	}
	iterFail := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{"p", "npm", "lodash", "lodash", ""}}, iterErr: errors.New("iter boom")}, nil
	}}
	if _, err := loadReadinessRegistryKeysForPackageIDs(t.Context(), iterFail, []string{"p"}); err == nil {
		t.Fatalf("iteration failure = nil error, want failure")
	}
	closeFail := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{"p", "npm", "lodash", "lodash", ""}}, closeErr: errors.New("close boom")}, nil
	}}
	if _, err := loadReadinessRegistryKeysForPackageIDs(t.Context(), closeFail, []string{"p"}); err == nil {
		t.Fatalf("close failure = nil error, want failure")
	}
	happy := &cov7TargetQueryer{onQuery: func(string) (db.Rows, error) {
		return &cov7TargetRows{rows: [][]any{{"p", "npm", "lodash", "lodash", ""}}}, nil
	}}
	keys, err = loadReadinessRegistryKeysForPackageIDs(t.Context(), happy, []string{"p"})
	if err != nil || len(keys["p"]) == 0 {
		t.Fatalf("load = (%v, %v), want lodash keys", keys, err)
	}
	resolved, err := resolveReadinessPackageID(t.Context(), happy, "npm://registry.npmjs.org/lodash")
	if err != nil || resolved.packageID != "npm://registry.npmjs.org/lodash" {
		t.Fatalf("package resolve = (%#v, %v), want lodash target", resolved, err)
	}
	if _, err := resolveReadinessPackageID(t.Context(), happy, "  "); err == nil {
		t.Fatalf("blank package ID = nil error, want identity error")
	}
}
