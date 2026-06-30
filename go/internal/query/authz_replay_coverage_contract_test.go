// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

func TestAuthorizationReplayCoverageContract(t *testing.T) {
	root := authzContractRepoRoot(t)
	authz, err := capabilitycatalog.LoadAuthorizationCatalog(filepath.Join(root, "specs", capabilitycatalog.AuthorizationFileName))
	if err != nil {
		t.Fatalf("LoadAuthorizationCatalog: %v", err)
	}
	ledger, err := replaycoverage.LoadAuthzProofLedger(filepath.Join(root, "specs", replaycoverage.AuthzProofFileName))
	if err != nil {
		t.Fatalf("LoadAuthzProofLedger: %v", err)
	}

	required := replaycoverage.EnumerateSupported(
		capabilitycatalog.SurfaceInventory{},
		nil,
		replaycoverage.ParserLedger{},
		capabilitycatalog.Matrix{},
		capabilitycatalog.ProductClaimLedger{},
		nil,
		authz,
	)
	proofs := map[string]replaycoverage.AuthzProofScenario{}
	for _, proof := range ledger.Scenarios {
		key := "authz_family:" + strings.TrimSpace(proof.Family) + ":" + strings.TrimSpace(proof.GrantMode)
		if _, dup := proofs[key]; dup {
			t.Fatalf("duplicate authorization replay proof %q", key)
		}
		proofs[key] = proof
	}

	for _, surface := range required {
		if surface.Registry != replaycoverage.RegistryAuthorizationCatalog {
			continue
		}
		proof, ok := proofs[surface.Key]
		if !ok {
			t.Fatalf("missing authorization replay proof for %s", surface.Key)
		}
		if strings.TrimSpace(proof.TestFile) == "" || strings.TrimSpace(proof.TestName) == "" {
			t.Fatalf("%s has blank test_file or test_name: %#v", surface.Key, proof)
		}
		if proof.TestName != "TestAuthorizationReplayCoverageContractRouteSamplesAllowScopedTokens" {
			t.Fatalf("%s test_name = %q, want route-dispatch proof", surface.Key, proof.TestName)
		}
		assertAuthzProofTestExists(t, root, proof.TestFile, proof.TestName)
		if len(proof.RouteSamples) == 0 {
			t.Fatalf("%s has no route_samples", surface.Key)
		}
		for _, sample := range proof.RouteSamples {
			method, path, ok := strings.Cut(strings.TrimSpace(sample), " ")
			if !ok || strings.TrimSpace(method) == "" || strings.TrimSpace(path) == "" {
				t.Fatalf("%s has malformed route sample %q", surface.Key, sample)
			}
			req := httptest.NewRequest(method, path, nil)
			if !scopedHTTPRouteSupportsTenantFilter(req) {
				t.Fatalf("%s route sample %q is not scoped-token allowlisted", surface.Key, sample)
			}
		}
	}
}

func TestAuthorizationReplayCoverageContractRouteSamplesAllowScopedTokens(t *testing.T) {
	root := authzContractRepoRoot(t)
	ledger, err := replaycoverage.LoadAuthzProofLedger(filepath.Join(root, "specs", replaycoverage.AuthzProofFileName))
	if err != nil {
		t.Fatalf("LoadAuthzProofLedger: %v", err)
	}

	for _, proof := range ledger.Scenarios {
		proof := proof
		for _, sample := range proof.RouteSamples {
			sample := sample
			t.Run(proof.Family+"/"+proof.GrantMode+"/"+sample, func(t *testing.T) {
				method, path := parseAuthzRouteSample(t, proof.Family, proof.GrantMode, sample)
				resolver := &fakeScopedTokenResolver{
					context: AuthContext{
						Mode:                 AuthModeScoped,
						TenantID:             "tenant-a",
						WorkspaceID:          "workspace-a",
						AllowedRepositoryIDs: []string{"repository:tenant-a/payments"},
						AllowedScopeIDs:      []string{"scope-a"},
					},
					ok: true,
				}
				called := false
				handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					if auth, ok := AuthContextFromContext(r.Context()); !ok || auth.Mode != AuthModeScoped {
						t.Fatalf("AuthContextFromContext = %#v, %t; want scoped auth context", auth, ok)
					}
					w.WriteHeader(http.StatusNoContent)
				}))
				req := httptest.NewRequest(method, path, nil)
				req.Header.Set("Authorization", "Bearer scoped-token")
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if got, want := rec.Code, http.StatusNoContent; got != want {
					t.Fatalf("%s status = %d, want %d; body = %s", sample, got, want, rec.Body.String())
				}
				if !called {
					t.Fatalf("%s did not reach route handler", sample)
				}
			})
		}
	}
}

func TestAuthorizationReplayCoverageContractRejectsBroadenedRepositoryScope(t *testing.T) {
	ctx := ContextWithAuthContext(context.Background(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repository:tenant-a/payments"},
	})
	filter := repositoryAccessFilterFromContext(ctx)
	got := filter.filterCatalogEntries([]RepositoryCatalogEntry{
		{ID: "repository:tenant-a/payments", Name: "payments"},
		{ID: "repository:tenant-b/billing", Name: "billing"},
	})

	if len(got) != 1 || got[0].ID != "repository:tenant-a/payments" {
		t.Fatalf("scoped filter broadened beyond in-grant repository: %#v", got)
	}
}

func authzContractRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func assertAuthzProofTestExists(t *testing.T, root, testFile, testName string) {
	t.Helper()
	root = filepath.Clean(root)
	path := filepath.Clean(filepath.Join(root, testFile))
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Fatalf("authz proof test file %q escapes repo root %q", testFile, root)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse authz proof test file %q: %v", testFile, err)
	}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name != nil && fn.Name.Name == testName {
			return
		}
	}
	t.Fatalf("authz proof test %s not found in %s", testName, testFile)
}

func parseAuthzRouteSample(t *testing.T, family, mode, sample string) (string, string) {
	t.Helper()
	method, path, ok := strings.Cut(strings.TrimSpace(sample), " ")
	if !ok || strings.TrimSpace(method) == "" || strings.TrimSpace(path) == "" {
		t.Fatalf("%s:%s has malformed route sample %q", family, mode, sample)
	}
	return strings.TrimSpace(method), strings.TrimSpace(path)
}
