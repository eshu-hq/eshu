// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
	"testing"
	"time"

	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
)

const composerLoggerKey = `package:acme/logging#Acme\Logging\Logger`

// TestReducerContentionGateActiveCodeCallSymbolLoaderAnchorsComposerKeys
// proves on real Postgres that a composer package key (#7601 PHP) loads only
// the active file facts of the scopes whose stored composer.json names the
// package: a nested monorepo manifest counts, a dirty scope (manifest tagged
// with a never-activated generation) is always scanned, and neither a
// composer.json under vendor/ nor a package.json carrying the same name
// anchors.
func TestReducerContentionGateActiveCodeCallSymbolLoaderAnchorsComposerKeys(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()

	// Producer: the nested manifest of a monorepo package names acme/logging.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:php-logging", "repository:r_php_logging", "generation-php-logging", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_php_logging", "composer.json", `{"name":"acme/monorepo"}`, "generation-php-logging", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_php_logging", "packages/logging/composer.json", `{"name":"acme/logging","autoload":{"psr-4":{"Acme\\Logging\\":"src/"}}}`, "generation-php-logging", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-php-logging", "scope:php-logging", "generation-php-logging", "packages/logging/src/Logger.php", "acme/logging", `Acme\Logging\Logger`, now)

	// Not producers, though each carries a definition with the same key.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:php-vendored", "repository:r_php_vendored", "generation-php-vendored", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_php_vendored", "composer.json", `{"name":"acme/app"}`, "generation-php-vendored", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_php_vendored", "vendor/acme/logging/composer.json", `{"name":"acme/logging"}`, "generation-php-vendored", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-php-vendored", "scope:php-vendored", "generation-php-vendored", "vendor/acme/logging/src/Logger.php", "acme/logging", `Acme\Logging\Logger`, now.Add(time.Second))
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:npm-lookalike", "repository:r_npm_lookalike", "generation-npm-lookalike", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_npm_lookalike", "package.json", `{"name":"acme/logging"}`, "generation-npm-lookalike", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-npm-lookalike", "scope:npm-lookalike", "generation-npm-lookalike", "index.js", "acme/logging", `Acme\Logging\Logger`, now.Add(2*time.Second))

	// Dirty: the stored composer.json was written by a generation that never
	// activated, so its name is unknown and the scope must be scanned.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:php-dirty", "repository:r_php_dirty", "generation-php-dirty", now)
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ('generation-php-dirty-refused', 'scope:php-dirty', 'snapshot', $1, $1, 'pending')`, now.Add(time.Minute)); err != nil {
		t.Fatalf("insert refused generation: %v", err)
	}
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_php_dirty", "composer.json", `{"name":"acme/renamed"}`, "generation-php-dirty-refused", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-php-dirty", "scope:php-dirty", "generation-php-dirty", "src/Logger.php", "acme/logging", `Acme\Logging\Logger`, now.Add(3*time.Second))

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{composerLoggerKey})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-php-logging", "fact-php-dirty"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
	if got, want := queryer.queries, []string{
		producerstore.ComposerManifestsQuery,
		listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("issued %d queries, want the composer.json read then the anchored scan only", len(got))
	}
	if got, want := queryer.args[1][4], []string{"scope:php-dirty", "scope:php-logging"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored scan producer scopes ($5) = %#v, want %#v", got, want)
	}
}

// TestReducerContentionGateActiveCodeCallSymbolLoaderKeepsNpmAndComposerNamesApart
// pins the name-space split on real Postgres: one request with an npm key and
// a composer key reads package.json for the first and composer.json for the
// second, then scans the union of their producers once.
func TestReducerContentionGateActiveCodeCallSymbolLoaderKeepsNpmAndComposerNamesApart(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()

	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:npm", "repository:r_npm", "generation-npm", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_npm", "package.json", `{"name":"@acme/logging"}`, "generation-npm", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-npm", "scope:npm", "generation-npm", "index.js", "@acme/logging", "Logger", now)
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:php", "repository:r_php", "generation-php", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_php", "composer.json", `{"name":"acme/logging"}`, "generation-php", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-php", "scope:php", "generation-php", "src/Logger.php", "acme/logging", `Acme\Logging\Logger`, now.Add(time.Second))
	// A composer.json named like the npm package anchors nothing for it.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:php-npm-name", "repository:r_php_npm_name", "generation-php-npm-name", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_php_npm_name", "composer.json", `{"name":"@acme/logging"}`, "generation-php-npm-name", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-php-npm-name", "scope:php-npm-name", "generation-php-npm-name", "index.js", "@acme/logging", "Logger", now.Add(2*time.Second))

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{
		"package:@acme/logging#Logger",
		composerLoggerKey,
	})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-npm", "fact-php"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
	if got, want := queryer.queries, []string{
		producerstore.PackageManifestsQuery,
		producerstore.ComposerManifestsQuery,
		listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("issued %d queries, want the package.json read, the composer.json read, then one anchored scan", len(got))
	}
	if got, want := queryer.args[2][4], []string{"scope:npm", "scope:php"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored scan producer scopes ($5) = %#v, want %#v", got, want)
	}
}

// TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsScanForUnpublishedComposerPackage
// proves a composer key no stored composer.json names runs only the manifest
// read and never the definition scan.
func TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsScanForUnpublishedComposerPackage(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:php-other", "repository:r_php_other", "generation-php-other", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_php_other", "composer.json", `{"name":"acme/other"}`, "generation-php-other", now)

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{composerLoggerKey})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded fact ids = %#v, want none", factIDs(loaded))
	}
	if got, want := queryer.queries, []string{producerstore.ComposerManifestsQuery}; !reflect.DeepEqual(got, want) {
		t.Fatalf("issued %d queries, want the composer.json read only", len(got))
	}
}
