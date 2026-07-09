// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// secretcryptoImportPath is the package this test asserts go/internal/query
// never imports (epic #4962 boundary: secretcrypto.Open is confined to
// login/authn packages such as oidclogin and samlauth; secretcrypto.Seal for
// provider-config writes is confined to the storage/postgres package, which
// holds the keyring — see identity_provider_config_writes.go's package doc
// comment). This test fails the build loudly, not just the intent, if that
// boundary is ever crossed from this package.
const secretcryptoImportPath = `"github.com/eshu-hq/eshu/go/internal/secretcrypto"`

// TestNoQueryFileImportsSecretcrypto proves no non-test .go file under
// go/internal/query imports secretcrypto. The read path (GET provider
// configs, provider config list, revision history) must never call
// secretcrypto.Open — the simplest, strictest, and most future-proof way to
// guarantee that is for this package to never import the package at all;
// sealing happens in storage/postgres (which holds the keyring) and opening
// happens in login/authn packages (oidclogin, samlauth), never here.
func TestNoQueryFileImportsSecretcrypto(t *testing.T) {
	t.Parallel()

	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob go/internal/query/*.go: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no .go files found in go/internal/query — glob pattern is broken")
	}

	fset := token.NewFileSet()
	checked := 0
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		checked++
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range file.Imports {
			if imp.Path.Value == secretcryptoImportPath {
				t.Errorf("%s imports secretcrypto: go/internal/query must never import it (Open is confined to login/authn packages, Seal to storage/postgres)", path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test .go files checked — glob or suffix filter is broken")
	}
}
