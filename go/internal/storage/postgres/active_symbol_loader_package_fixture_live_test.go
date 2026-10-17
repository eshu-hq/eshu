// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8244 disposable PostgreSQL 18 proof that a package.json under a test, tests, fixtures, testdata, or examples directory is not a package producer (a real manifest of the same name still anchors, substring look-alikes still anchor), and that the one fixture pattern evaluates identically in PostgreSQL ~ and Go regexp on a path corpus including the long s; enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
)

// TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsFixturePackageManifests
// proves on real Postgres that a package.json under a fixture-style directory
// (#8244) is not a producer: a scope whose only manifest naming the package
// sits under a tests or fixtures directory is not scanned, while a scope with a
// real manifest of the same name still is, and a node_modules backup copy stays
// excluded as before.
func TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsFixturePackageManifests(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	const key = "package:acme-lib#Greeter"

	// Real producer, plus a fixture manifest that repeats its name.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:real", "repository:r_real", "generation-real", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_real", "packages/lib/package.json", `{"name":"acme-lib"}`, "generation-real", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_real", "packages/lib/tests/fixtures/app/package.json", `{"name":"acme-lib"}`, "generation-real", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-real", "scope:real", "generation-real", "packages/lib/src/index.js", "acme-lib", "Greeter", now)

	// Not producers: the only manifest naming the package is scaffolding.
	for i, relativePath := range []string{
		"tests/package.json",
		"Test/app/package.json",
		"docs/Examples/demo/package.json",
		"testdata/pkg/package.json",
		"TESTS/FIXTURES/package.json",
		"packages/old/node_modules.bak/acme-lib/package.json",
	} {
		suffix := string(rune('a' + i))
		scope, repo, generation := "scope:scaffold-"+suffix, "repository:r_scaffold_"+suffix, "generation-scaffold-"+suffix
		seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, scope, repo, generation, now)
		seedActiveCodeCallSymbolManifest(t, ctx, database, repo, relativePath, `{"name":"acme-lib"}`, generation, now)
		seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-scaffold-"+suffix, scope, generation, "src/index.js", "acme-lib", "Greeter", now.Add(time.Second))
	}

	// Substring look-alikes of the fixture names are real producers.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:testing-dir", "repository:r_testing_dir", "generation-testing-dir", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_testing_dir", "packages/testing/package.json", `{"name":"acme-lib"}`, "generation-testing-dir", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-testing-dir", "scope:testing-dir", "generation-testing-dir", "packages/testing/src/index.js", "acme-lib", "Greeter", now.Add(2*time.Second))

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{key})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-real", "fact-testing-dir"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
	if got := queryer.queries[0]; got != producerstore.PackageManifestsQuery {
		t.Fatalf("first query is not the package.json manifest read")
	}
	if got, want := queryer.args[1][4], []string{"scope:real", "scope:testing-dir"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored scan producer scopes ($5) = %#v, want %#v", got, want)
	}
}

// TestFixtureDirPatternAgreesBetweenPostgresAndGo evaluates the one pattern
// constant with PostgreSQL's ~ and with Go's regexp on a path corpus that
// includes the cases where a case-fold flag would disagree (the long s,
// U+017F), so the SQL anchor and the parser stamp cannot drift apart (#8244).
func TestFixtureDirPatternAgreesBetweenPostgresAndGo(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)

	for _, path := range []string{
		"package.json", "packages/core/package.json", "tests/package.json",
		"Tests/Fixtures/app/package.json", "FIXTURES/package.json", "TESTS/package.json",
		"Test/package.json", "test/app/package.json", "testdata/pkg/package.json",
		"TestData/pkg/package.json", "docs/Examples/demo/package.json", "examples/package.json",
		"packages/testing/package.json", "packages/attestation/package.json",
		"packages/latests/package.json", "packages/contest/package.json",
		"packages/fixtures-lib/package.json", "packages/my-examples/package.json",
		"packages/latest/package.json", "packages/tester/package.json",
		"a/teſts/package.json", "a/TEſTS/package.json", "a/Teſt/package.json",
		"a/ſixtures/package.json", "a/exampleS/package.json", "a/EXAMPLES/package.json",
		"packages/tests", "packages/testsx/package.json", "xtests/package.json",
	} {
		var inFixtureSQL bool
		if err := database.QueryRowContext(ctx, `SELECT $1::text ~ $2::text`, path, shared.FixtureDirPattern).Scan(&inFixtureSQL); err != nil {
			t.Fatalf("evaluate pattern for %q: %v", path, err)
		}
		if got := shared.InFixtureDir(path); got != inFixtureSQL {
			t.Errorf("path %q: Go InFixtureDir = %t, PostgreSQL ~ = %t", path, got, inFixtureSQL)
		}
	}
}
