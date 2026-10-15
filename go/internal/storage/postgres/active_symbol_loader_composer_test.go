// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
)

// TestFactStoreLoadActiveCodeCallSymbolDefinitionFactsAnchorsComposerKeysOnComposerManifests
// pins the #7601 PHP leg: a package:<vendor>/<name>#<FQCN> key names a composer
// package, so its producers are the scopes whose stored composer.json names it.
// Reading package.json for it would anchor on nothing (npm names are never
// <vendor>/<name> without an @scope) or on an unrelated npm package.
func TestFactStoreLoadActiveCodeCallSymbolDefinitionFactsAnchorsComposerKeysOnComposerManifests(t *testing.T) {
	t.Parallel()

	composerKey := `package:acme/logging#Acme\Logging\Logger`
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"scope:producer", sql.NullString{String: `{"name": "acme/logging", "autoload": {"psr-4": {"Acme\\Logging\\": "src/"}}}`, Valid: true}, "clean"},
				{"scope:noise", sql.NullString{String: `{"name": "acme/other"}`, Valid: true}, "clean"},
			}},
			{rows: [][]any{codeCallDefinitionScanRow("fact-php-anchored")}},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(context.Background(), []string{composerKey})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-php-anchored"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("issued %d queries, want the composer.json manifest read then the anchored scan", got)
	}
	manifestQuery := db.queries[0].query
	if !strings.Contains(manifestQuery, "relative_path = 'composer.json'") ||
		!strings.Contains(manifestQuery, "relative_path LIKE '%/composer.json'") {
		t.Fatalf("first query must read composer.json manifests:\n%s", manifestQuery)
	}
	if strings.Contains(manifestQuery, "package.json") {
		t.Fatalf("composer keys must not read package.json manifests:\n%s", manifestQuery)
	}
	assertCodeCallManifestShape(t, manifestQuery, "relative_path = 'composer.json'", "relative_path LIKE '%/composer.json'")
	if got, want := db.queries[1].query, listAnchoredActiveCodeCallSymbolDefinitionFactsQuery; got != want {
		t.Fatalf("second query is the anchored scan:\n%s", got)
	}
	if got, want := db.queries[1].args[4], []string{"scope:producer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored scan producer scopes ($5) = %#v, want %#v", got, want)
	}
}
