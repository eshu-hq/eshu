// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// splitExemptionReceiver divides a canonical method-style symbol into its
// receiver type and method names. It reports false for plain functions
// and for closures (Type.Method.func1 has no source declaration to check:
// the declaration belongs to the outer method, not the closure frame).
func splitExemptionReceiver(symbol string) (recv, method string, isMethod bool) {
	rest := symbol
	if strings.HasPrefix(rest, "(") {
		end := strings.Index(rest, ")")
		if end < 0 || end+1 >= len(rest) || rest[end+1] != '.' {
			return "", "", false
		}
		recv, rest = rest[1:end], rest[end+2:]
	} else {
		var found bool
		recv, rest, found = strings.Cut(rest, ".")
		if !found {
			return "", "", false
		}
	}
	recv = strings.TrimPrefix(recv, "*")
	if i := strings.Index(recv, "["); i >= 0 {
		recv = recv[:i]
	}
	if i := strings.Index(rest, "."); i >= 0 {
		return "", "", false
	}
	if i := strings.Index(rest, "["); i >= 0 {
		rest = rest[:i]
	}
	if recv == "" || rest == "" {
		return "", "", false
	}
	return recv, rest, true
}

func builderManifestFixture() BuilderManifest {
	return BuilderManifest{
		Version: 1,
		Builders: []StatementBuilderCoverage{
			{
				File: "writer.go",
				Builders: []StatementBuilder{
					{
						Symbol:    "buildUpsert",
						Count:     1,
						Operation: "sourcecypher.OperationCanonicalUpsert",
						Variants: []StatementVariant{
							{Template: "MATCH (n) RETURN n"},
							{Template: "MATCH (m) RETURN m"},
						},
						SourceDigest: "aaa",
					},
					{
						Symbol: "buildDynamic",
						Count:  1,
						Variants: []StatementVariant{
							{Fragments: []string{"MATCH (n:", ") RETURN n"}},
						},
						SourceDigest: "bbb",
					},
				},
			},
		},
	}
}

func discoveredBuildersFixture() []StatementBuilderCoverage {
	manifest := builderManifestFixture()
	return manifest.Builders
}

func TestValidateBuilderManifestAcceptsMatchingDiscovery(t *testing.T) {
	if err := ValidateBuilderManifest(builderManifestFixture(), discoveredBuildersFixture()); err != nil {
		t.Fatalf("ValidateBuilderManifest() error = %v", err)
	}
}

func TestValidateBuilderManifestRejectsUnregisteredBuilder(t *testing.T) {
	discovered := append(discoveredBuildersFixture(), StatementBuilderCoverage{
		File:     "writer.go",
		Builders: []StatementBuilder{{Symbol: "buildNew", Count: 1, SourceDigest: "ccc"}},
	})
	err := ValidateBuilderManifest(builderManifestFixture(), discovered)
	if err == nil || !strings.Contains(err.Error(), "unregistered statement builder writer.go:buildNew") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want unregistered builder", err)
	}
}

func TestValidateBuilderManifestRejectsStaleRegistration(t *testing.T) {
	manifest := builderManifestFixture()
	manifest.Builders[0].Builders = append(manifest.Builders[0].Builders, StatementBuilder{
		Symbol:       "buildRemoved",
		Count:        1,
		SourceDigest: "ddd",
	})
	err := ValidateBuilderManifest(manifest, discoveredBuildersFixture())
	if err == nil || !strings.Contains(err.Error(), "stale statement builder registration writer.go:buildRemoved") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want stale registration", err)
	}
}

func TestValidateBuilderManifestRejectsCountMismatch(t *testing.T) {
	discovered := discoveredBuildersFixture()
	discovered[0].Builders[0].Count = 3
	err := ValidateBuilderManifest(builderManifestFixture(), discovered)
	if err == nil || !strings.Contains(err.Error(), "discovered build count 3, manifest requires 1") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want count mismatch", err)
	}
}

func TestValidateBuilderManifestRejectsDigestMismatch(t *testing.T) {
	discovered := discoveredBuildersFixture()
	discovered[0].Builders[0].SourceDigest = "changed"
	err := ValidateBuilderManifest(builderManifestFixture(), discovered)
	if err == nil || !strings.Contains(err.Error(), "source_sha256 does not match production symbol") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want digest mismatch", err)
	}
}

func TestValidateBuilderManifestRejectsVariantDrift(t *testing.T) {
	discovered := discoveredBuildersFixture()
	discovered[0].Builders[0].Variants = discovered[0].Builders[0].Variants[:1]
	err := ValidateBuilderManifest(builderManifestFixture(), discovered)
	if err == nil || !strings.Contains(err.Error(), "statement variants do not match production symbol") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want variant drift", err)
	}
}

func TestValidateBuilderManifestRejectsFragmentDrift(t *testing.T) {
	discovered := discoveredBuildersFixture()
	discovered[0].Builders[1].Variants[0].Fragments = []string{"MATCH (m:"}
	err := ValidateBuilderManifest(builderManifestFixture(), discovered)
	if err == nil || !strings.Contains(err.Error(), "statement variants do not match production symbol") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want fragment drift", err)
	}
}

func TestValidateBuilderManifestRejectsBlankExemption(t *testing.T) {
	manifest := builderManifestFixture()
	manifest.Builders[0].Builders[0].Exempt = "  "
	err := ValidateBuilderManifest(manifest, discoveredBuildersFixture())
	if err == nil || !strings.Contains(err.Error(), "exemption requires a reason") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want blank exemption rejection", err)
	}
}

func TestValidateBuilderManifestRejectsBlankReadExemption(t *testing.T) {
	manifest := builderManifestFixture()
	manifest.ReadExemptions = []ReadExemption{{
		Callsite: "internal/queryplan/statement_manifest.go:ValidateBuilderManifest",
		Anchor:   "read exemption requires a reason",
	}}
	err := ValidateBuilderManifest(manifest, discoveredBuildersFixture())
	if err == nil || !strings.Contains(err.Error(), "read exemption requires a reason") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want blank read exemption rejection", err)
	}
}

// TestValidateBuilderManifestAcceptsIdentityExemption pins the #7233
// contract: a read exemption names a builder identity (go-relative
// path:symbol of the direct Run/RunSingle caller) plus a stable anchor
// fragment, not the full statement text.
func TestValidateBuilderManifestAcceptsIdentityExemption(t *testing.T) {
	manifest := builderManifestFixture()
	manifest.ReadExemptions = []ReadExemption{{
		Callsite: "internal/queryplan/statement_manifest.go:ValidateBuilderManifest",
		Anchor:   "read exemption requires a reason",
		Reason:   "miss-path read over an absent entity",
	}}
	if err := ValidateBuilderManifest(manifest, discoveredBuildersFixture()); err != nil {
		t.Fatalf("ValidateBuilderManifest() error = %v, want identity exemption accepted", err)
	}
}

func TestValidateBuilderManifestRejectsExemptionWithoutCallsite(t *testing.T) {
	manifest := builderManifestFixture()
	manifest.ReadExemptions = []ReadExemption{{
		Anchor: "MATCH (n:Admin) WHERE n.id = $id",
		Reason: "admin-only read",
	}}
	err := ValidateBuilderManifest(manifest, discoveredBuildersFixture())
	if err == nil || !strings.Contains(err.Error(), "read exemption requires a callsite") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want missing-callsite rejection", err)
	}
}

func TestValidateBuilderManifestRejectsMalformedExemptionCallsite(t *testing.T) {
	for _, callsite := range []string{
		"nocolon",
		"internal/queryplan/does-not-exist.go:SomeSymbol",
		":EmptyPath",
	} {
		manifest := builderManifestFixture()
		manifest.ReadExemptions = []ReadExemption{{
			Callsite: callsite,
			Anchor:   "MATCH (n:Admin) WHERE n.id = $id",
			Reason:   "admin-only read",
		}}
		if err := ValidateBuilderManifest(manifest, discoveredBuildersFixture()); err == nil {
			t.Fatalf("ValidateBuilderManifest() accepted malformed callsite %q", callsite)
		}
	}
}

func TestValidateBuilderManifestRejectsShortExemptionAnchor(t *testing.T) {
	manifest := builderManifestFixture()
	manifest.ReadExemptions = []ReadExemption{{
		Callsite: "internal/queryplan/statement_manifest.go:ValidateBuilderManifest",
		Anchor:   "MATCH (n)",
		Reason:   "admin-only read",
	}}
	err := ValidateBuilderManifest(manifest, discoveredBuildersFixture())
	if err == nil || !strings.Contains(err.Error(), "read exemption anchor") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want short-anchor rejection", err)
	}
}

// TestLoadBuilderManifestCanonicalizesBareReceiver (#7233): the Go
// runtime renders value-receiver methods bare (Type.Method) while authors
// write the method-expression form ((Type).Method), so loading must
// canonicalize to the parenthesized form — otherwise a manifest entry
// that names the same builder as a recording never matches it (observed:
// EnumerateProjectedSourceEdges recorded bare while the manifest used
// parens, failing both always-empty and stale).
func TestLoadBuilderManifestCanonicalizesBareReceiver(t *testing.T) {
	manifest, err := LoadBuilderManifest("testdata/statement-builders.yaml")
	if err != nil {
		t.Fatalf("LoadBuilderManifest() error = %v", err)
	}
	for _, exemption := range manifest.ReadExemptions {
		if !strings.HasPrefix(exemption.Callsite, "internal/query/secrets/grant_posture.go:") {
			continue
		}
		want := "internal/query/secrets/grant_posture.go:(GraphIAMGrantPostureStore).groupedGrantCounts"
		if exemption.Callsite != want {
			t.Fatalf("loaded callsite = %q, want canonical %q", exemption.Callsite, want)
		}
		return
	}
	t.Fatal("bare-receiver grant_posture exemption not found in manifest")
}

// TestCanonicalCallsiteSymbol pins the receiver-form contract both
// capture and manifest load apply: value receivers wrap bare, pointer
// receivers keep their star, plain functions pass through, generic
// brackets do not split the receiver, and closures parenthesize the
// function name.
func TestCanonicalCallsiteSymbol(t *testing.T) {
	cases := map[string]string{
		"Reader.Enumerate":                   "(Reader).Enumerate",
		"(*Store).EdgesBySourceTool":         "(*Store).EdgesBySourceTool",
		"(Reader).Enumerate":                 "(Reader).Enumerate",
		"countRepos":                         "countRepos",
		"Store[T].Get":                       "(Store[T]).Get",
		"Store[github.com/x/y.Type].Get":     "(Store[github.com/x/y.Type]).Get",
		"Reader.Enumerate.func1":             "(Reader).Enumerate.func1",
		"neo4jWorkloadDependencyLookup.List": "(neo4jWorkloadDependencyLookup).List",
	}
	for in, want := range cases {
		if got := CanonicalCallsiteSymbol(in); got != want {
			t.Errorf("CanonicalCallsiteSymbol(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestReadExemptionReceiversMatchSource (#7233) guards the bug class that
// broke the first identity-keyed rollout: an exemption naming (*T).M
// while the source declares a value receiver (or vice versa) can never
// match a recording — pointer receivers record (*T).M, value receivers
// T.M — so the read stays always-empty AND the exemption goes stale.
// Every method-style manifest callsite must match its source declaration
// with the same star.
func TestReadExemptionReceiversMatchSource(t *testing.T) {
	manifest, err := LoadBuilderManifest("testdata/statement-builders.yaml")
	if err != nil {
		t.Fatalf("LoadBuilderManifest() error = %v", err)
	}
	goDir, err := GoDir()
	if err != nil {
		t.Fatal(err)
	}
	declPattern := regexp.MustCompile(`func\s*\(\s*\w+\s+(\*?)([A-Za-z0-9_]+)[^)]*\)\s*([A-Za-z0-9_]+)[\(\[]`)
	for _, exemption := range manifest.ReadExemptions {
		path, symbol, ok := splitCallsite(exemption.Callsite)
		if !ok {
			t.Errorf("exemption callsite does not split: %q", exemption.Callsite)
			continue
		}
		recv, method, isMethod := splitExemptionReceiver(symbol)
		if !isMethod {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(goDir, filepath.FromSlash(path)))
		if err != nil {
			t.Errorf("read exemption file: %v", err)
			continue
		}
		found := false
		for _, match := range declPattern.FindAllStringSubmatch(string(raw), -1) {
			if match[2] != recv || match[3] != method {
				continue
			}
			found = true
			if (match[1] == "*") != strings.HasPrefix(symbol, "(*") {
				t.Errorf("exemption %q: manifest star does not match source receiver", exemption.Callsite)
			}
		}
		if !found {
			t.Errorf("exemption %q: no such method declaration in %s", exemption.Callsite, path)
		}
	}
}

// TestStatementBuildersManifestMatchesProduction pins the checked-in
// manifest against fresh discovery over the real tree: adding, removing,
// or editing a builder without regenerating the manifest fails here, and
// any discovery nondeterminism surfaces as a mismatch on re-runs.
func TestStatementBuildersManifestMatchesProduction(t *testing.T) {
	manifest, err := LoadBuilderManifest("testdata/statement-builders.yaml")
	if err != nil {
		t.Fatalf("LoadBuilderManifest() error = %v", err)
	}
	discovered, err := DiscoverStatementBuilders("../..")
	if err != nil {
		t.Fatalf("DiscoverStatementBuilders() error = %v", err)
	}
	if err := ValidateBuilderManifest(manifest, discovered); err != nil {
		t.Fatalf("production statement builders manifest: %v", err)
	}
}
