// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	gomodparser "github.com/eshu-hq/eshu/go/internal/parser/gomod"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestBuildPackageConsumptionDecisionsAdmitsParsedGoModRequire wires the
// gomod parser output through BuildPackageConsumptionDecisions to prove the
// Go module evidence path is reducer-grade. The reducer must admit a
// consumption decision when a package_registry.package fact and a parsed
// go.mod require directive name the same module, and the decision must
// preserve direct/indirect, source-truth require version, and the
// repository identity.
func TestBuildPackageConsumptionDecisionsAdmitsParsedGoModRequire(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	rows := parseGoModRowsForTest(t, `module example.com/app

go 1.22

require (
	golang.org/x/text v0.3.7
	golang.org/x/sys v0.10.0 // indirect
)
`)

	envelopes := []facts.Envelope{
		packageRegistryPackageFact(
			"gomod://proxy.golang.org/golang.org/x/text",
			"gomod",
			"golang.org/x/text",
			"golang.org/x",
			observedAt,
		),
		packageRegistryPackageFact(
			"gomod://proxy.golang.org/golang.org/x/sys",
			"gomod",
			"golang.org/x/sys",
			"golang.org/x",
			observedAt,
		),
		packageSourceRepositoryFact("repo-go", "service", "https://github.com/acme/service", false, observedAt),
	}
	envelopes = append(
		envelopes,
		goModContentEntityEnvelope(t, "repo-go", "service", "go.mod", "golang.org/x/text", rows, observedAt),
		goModContentEntityEnvelope(t, "repo-go", "service", "go.mod", "golang.org/x/sys", rows, observedAt),
	)

	decisions := BuildPackageConsumptionDecisions(envelopes)
	if got, want := len(decisions), 2; got != want {
		t.Fatalf("len(decisions) = %d, want %d (one per required module)", got, want)
	}
	decisionsByPackage := map[string]PackageConsumptionDecision{}
	for _, decision := range decisions {
		decisionsByPackage[decision.PackageID] = decision
	}

	text, ok := decisionsByPackage["gomod://proxy.golang.org/golang.org/x/text"]
	if !ok {
		t.Fatalf("missing consumption decision for direct require golang.org/x/text: %#v", decisionsByPackage)
	}
	if got, want := text.RelativePath, "go.mod"; got != want {
		t.Fatalf("RelativePath = %q, want %q", got, want)
	}
	if got, want := text.DependencyRange, "v0.3.7"; got != want {
		t.Fatalf("DependencyRange = %q, want source-truth require version %q", got, want)
	}
	if text.DirectDependency == nil || !*text.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want true for direct require", text.DirectDependency)
	}
	if got, want := text.ManifestSection, "require"; got != want {
		t.Fatalf("ManifestSection = %q, want %q so direct and indirect stay distinguishable", got, want)
	}
	if !reflect.DeepEqual(text.DependencyPath, []string{"golang.org/x/text"}) {
		t.Fatalf("DependencyPath = %#v, want direct module path", text.DependencyPath)
	}

	sys, ok := decisionsByPackage["gomod://proxy.golang.org/golang.org/x/sys"]
	if !ok {
		t.Fatalf("missing consumption decision for indirect require golang.org/x/sys: %#v", decisionsByPackage)
	}
	if sys.DirectDependency == nil || *sys.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want false for // indirect", sys.DirectDependency)
	}
	if got, want := sys.ManifestSection, "require-indirect"; got != want {
		t.Fatalf("ManifestSection = %q, want %q so reducer can scope to direct dependencies", got, want)
	}
}

// TestBuildPackageConsumptionDecisionsRejectsGoSumChecksumRows proves the
// checksum-only ambiguity rule. go.sum entries produced by the gomod parser
// MUST NOT be admitted as consumption decisions because go.sum records every
// version any tool has ever verified, not the currently selected version.
// Without this guard, a stale or evicted version would be reported as
// affected.
func TestBuildPackageConsumptionDecisionsRejectsGoSumChecksumRows(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 12, 5, 0, 0, time.UTC)
	rows := parseGoSumRowsForTest(t, `golang.org/x/text v0.3.7 h1:olpwvP2KacW1ZWvsR7uQhoyTYvKAupfQrRGBFM352Gk=
golang.org/x/text v0.3.7/go.mod h1:5Zf9MlPGSHRzGAY0xqgNYbsmkNibR7P++ZRPSqVbA0Q=
`)

	envelopes := []facts.Envelope{
		packageRegistryPackageFact(
			"gomod://proxy.golang.org/golang.org/x/text",
			"gomod",
			"golang.org/x/text",
			"golang.org/x",
			observedAt,
		),
		packageSourceRepositoryFact("repo-go", "service", "https://github.com/acme/service", false, observedAt),
	}
	for _, row := range rows {
		envelopes = append(envelopes, goSumContentEntityEnvelope(t, "repo-go", "service", "go.sum", row, observedAt))
	}

	decisions := BuildPackageConsumptionDecisions(envelopes)
	if len(decisions) != 0 {
		t.Fatalf("BuildPackageConsumptionDecisions admitted %d decisions from go.sum checksum-only evidence; expected zero so checksum-only ambiguity stays explicit: %#v", len(decisions), decisions)
	}
}

// TestBuildPackageConsumptionDecisionsRejectsGoModReplaceAndExclude proves
// that replace/exclude directives are not admitted as consumption. They are
// emitted by the parser for audit visibility but must never declare that the
// repository consumes the replaced or excluded coordinate.
func TestBuildPackageConsumptionDecisionsRejectsGoModReplaceAndExclude(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 12, 10, 0, 0, time.UTC)
	rows := parseGoModRowsForTest(t, `module example.com/app

go 1.22

require example.com/keeper v1.0.0

replace example.com/standalone-replace => example.com/new v1.5.0

exclude example.com/bad v1.0.0
`)

	envelopes := []facts.Envelope{
		packageRegistryPackageFact(
			"gomod://proxy.golang.org/example.com/keeper",
			"gomod",
			"example.com/keeper",
			"example.com",
			observedAt,
		),
		packageRegistryPackageFact(
			"gomod://proxy.golang.org/example.com/standalone-replace",
			"gomod",
			"example.com/standalone-replace",
			"example.com",
			observedAt,
		),
		packageRegistryPackageFact(
			"gomod://proxy.golang.org/example.com/bad",
			"gomod",
			"example.com/bad",
			"example.com",
			observedAt,
		),
		packageSourceRepositoryFact("repo-go", "service", "https://github.com/acme/service", false, observedAt),
	}
	for _, row := range rows {
		name, _ := row["name"].(string)
		envelopes = append(envelopes, goModContentEntityEnvelopeForRow(t, "repo-go", "service", "go.mod", name, row, observedAt))
	}

	decisions := BuildPackageConsumptionDecisions(envelopes)
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d (only the require entry is consumption)", got, want)
	}
	if got, want := decisions[0].PackageID, "gomod://proxy.golang.org/example.com/keeper"; got != want {
		t.Fatalf("admitted decision PackageID = %q, want require-only %q; replace/exclude leaked as consumption", got, want)
	}
}

// TestBuildPackageConsumptionDecisionsRejectsMalformedGoModEvidence proves
// that a malformed go.mod must keep missing/ambiguous module evidence as
// missing — the reducer must produce zero consumption decisions when the
// parser only emitted a malformed-state envelope.
func TestBuildPackageConsumptionDecisionsRejectsMalformedGoModEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 12, 15, 0, 0, time.UTC)
	rows := parseGoModRowsForTest(t, `not a valid go.mod`)

	envelopes := []facts.Envelope{
		packageRegistryPackageFact(
			"gomod://proxy.golang.org/example.com/whatever",
			"gomod",
			"example.com/whatever",
			"example.com",
			observedAt,
		),
		packageSourceRepositoryFact("repo-go", "service", "https://github.com/acme/service", false, observedAt),
	}
	for _, row := range rows {
		name, _ := row["name"].(string)
		envelopes = append(envelopes, goModContentEntityEnvelopeForRow(t, "repo-go", "service", "go.mod", name, row, observedAt))
	}

	decisions := BuildPackageConsumptionDecisions(envelopes)
	if len(decisions) != 0 {
		t.Fatalf("BuildPackageConsumptionDecisions admitted %d decisions from a malformed go.mod; missing evidence must remain missing: %#v", len(decisions), decisions)
	}
}

func parseGoModRowsForTest(t *testing.T, body string) []map[string]any {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write go.mod fixture: %v", err)
	}
	payload, err := gomodparser.Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("gomodparser.Parse(go.mod): %v", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	return rows
}

func parseGoSumRowsForTest(t *testing.T, body string) []map[string]any {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "go.sum")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write go.sum fixture: %v", err)
	}
	payload, err := gomodparser.Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("gomodparser.Parse(go.sum): %v", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	return rows
}

func goModContentEntityEnvelope(
	t *testing.T,
	repoID, repoName, relativePath, moduleName string,
	parsedRows []map[string]any,
	observedAt time.Time,
) facts.Envelope {
	t.Helper()

	for _, row := range parsedRows {
		if name, _ := row["name"].(string); name == moduleName {
			if kind, _ := row["config_kind"].(string); kind == "dependency" {
				return goModContentEntityEnvelopeForRow(t, repoID, repoName, relativePath, moduleName, row, observedAt)
			}
		}
	}
	t.Fatalf("dependency row for module %q missing from parsed go.mod rows", moduleName)
	return facts.Envelope{}
}

func goModContentEntityEnvelopeForRow(
	t *testing.T,
	repoID, repoName, relativePath, moduleName string,
	row map[string]any,
	observedAt time.Time,
) facts.Envelope {
	t.Helper()

	metadata := map[string]any{
		"config_kind":     row["config_kind"],
		"package_manager": row["package_manager"],
		"section":         row["section"],
		"value":           row["value"],
	}
	if path, ok := row["dependency_path"].([]string); ok {
		metadata["dependency_path"] = path
	}
	if depth, ok := row["dependency_depth"].(int); ok {
		metadata["dependency_depth"] = depth
	}
	if direct, ok := row["direct_dependency"].(bool); ok {
		metadata["direct_dependency"] = direct
	}
	return facts.Envelope{
		FactID:        "manifest-dep:" + repoID + ":" + moduleName + ":" + relativePath,
		FactKind:      factKindContentEntity,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "git"},
		StableFactKey: "content_entity:" + repoID + ":" + moduleName,
		Payload: map[string]any{
			"repo_id":         repoID,
			"relative_path":   relativePath,
			"entity_type":     "Variable",
			"entity_name":     moduleName,
			"entity_metadata": metadata,
			"repo_name":       repoName,
		},
	}
}

func goSumContentEntityEnvelope(
	t *testing.T,
	repoID, repoName, relativePath string,
	row map[string]any,
	observedAt time.Time,
) facts.Envelope {
	t.Helper()

	name, _ := row["name"].(string)
	metadata := map[string]any{
		"config_kind":     row["config_kind"],
		"package_manager": row["package_manager"],
		"section":         row["section"],
		"value":           row["value"],
		"ambiguous":       row["ambiguous"],
		"checksum":        row["checksum"],
		"checksum_kind":   row["checksum_kind"],
	}
	if path, ok := row["dependency_path"].([]string); ok {
		metadata["dependency_path"] = path
	}
	if depth, ok := row["dependency_depth"].(int); ok {
		metadata["dependency_depth"] = depth
	}
	kind, _ := row["checksum_kind"].(string)
	return facts.Envelope{
		FactID:        "go-sum:" + repoID + ":" + name + ":" + kind,
		FactKind:      factKindContentEntity,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "git"},
		StableFactKey: "content_entity:" + repoID + ":" + name + ":" + kind,
		Payload: map[string]any{
			"repo_id":         repoID,
			"relative_path":   relativePath,
			"entity_type":     "Variable",
			"entity_name":     name,
			"entity_metadata": metadata,
			"repo_name":       repoName,
		},
	}
}
