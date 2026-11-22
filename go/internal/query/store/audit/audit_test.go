// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit_test

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/query/store/audit"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", ".."))
}

func declaredStores(t *testing.T) map[string][]string {
	t.Helper()
	matrix, err := capabilitycatalog.LoadMatrix(filepath.Join(repoRoot(t), "specs"))
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	out := make(map[string][]string, len(matrix.Capabilities))
	for _, capability := range matrix.Capabilities {
		out[capability.Capability] = capability.AnsweringStores
	}
	return out
}

// updateWaiversEnv rewrites the two waiver files from the current tree. The
// rewrite records what is unprobed or unowned today; it never removes a need.
const updateWaiversEnv = "ESHU_UPDATE_STOREAUDIT_WAIVERS"

// probeWaiverCeiling and censusWaiverCeiling pin the waiver lists. A list may
// only shrink: adding a waiver fails the ceiling, and removing one fails until
// the ceiling is lowered to match, so a shrink is never silently undone.
const (
	probeWaiverCeiling  = 155
	censusWaiverCeiling = 18
)

func waiverPath(name string) string { return filepath.Join("testdata", name) }

func readWaivers(t *testing.T, name string) []string {
	t.Helper()
	file, err := os.Open(waiverPath(name))
	if err != nil {
		t.Fatalf("open %s: %v (regenerate with %s=1)", name, err, updateWaiversEnv)
	}
	defer func() { _ = file.Close() }()
	var out []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func writeWaivers(t *testing.T, name, header string, entries []string) {
	t.Helper()
	sort.Strings(entries)
	body := header + "\n" + strings.Join(entries, "\n") + "\n"
	if err := os.WriteFile(waiverPath(name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestProbedCapabilitiesReadOnlyDeclaredStores is the honesty audit: every
// statement a probe drives must read a subset of the stores the capability
// declares in the matrix.
func TestProbedCapabilitiesReadOnlyDeclaredStores(t *testing.T) {
	declared := declaredStores(t)
	for _, p := range probes() {
		stores, ok := declared[p.capability]
		if !ok {
			t.Errorf("probe names capability %q, which is not in the matrix", p.capability)
			continue
		}
		recorder := &audit.Recorder{}
		p.run(context.Background(), recorder)
		if len(recorder.Statements()) == 0 {
			t.Errorf("%s / %s issued no statement: the probe drives nothing", p.capability, p.variant)
			continue
		}
		used := audit.Classify(recorder.Statements()...)
		for _, violation := range audit.Undeclared(stores, used) {
			t.Errorf("%s / %s reads %s but declares %v", p.capability, p.variant, violation, stores)
		}
	}
}

// TestAuditCatchesTheAdvisoryEvidenceDeclaration is the audit's first real
// find. Before #10880 PR-B0 the matrix declared supply_chain.advisory_evidence.list
// as fact_records_collector only, while its repo_probe and impact_candidates
// CTEs read reducer_supply_chain_impact_finding. The audit fails on that
// declaration and passes on the corrected one.
func TestAuditCatchesTheAdvisoryEvidenceDeclaration(t *testing.T) {
	const capability = "supply_chain.advisory_evidence.list"
	var used audit.Used
	for _, p := range probes() {
		if p.capability != capability {
			continue
		}
		recorder := &audit.Recorder{}
		p.run(context.Background(), recorder)
		for store, evidence := range audit.Classify(recorder.Statements()...).Stores {
			if used.Stores == nil {
				used.Stores = map[string]string{}
			}
			used.Stores[store] = evidence
		}
	}
	if len(used.Stores) == 0 {
		t.Fatalf("no probe drives %s", capability)
	}
	if got := audit.Undeclared([]string{audit.StoreFactRecordsCollector}, used); len(got) == 0 ||
		got[0].Store != audit.StoreFactRecordsReducer {
		t.Fatalf("RED: the pre-B0 declaration %v passed the audit, want a fact_records_reducer violation; got %v",
			[]string{audit.StoreFactRecordsCollector}, got)
	}
	if got := audit.Undeclared(declaredStores(t)[capability], used); len(got) != 0 {
		t.Fatalf("GREEN: the matrix declaration still fails the audit: %v", got)
	}
}

// TestEveryCapabilityIsProbedOrWaived runs the coverage ratchet over the matrix
// and the probe table: a capability with no probe and no waiver fails, a stale
// or duplicate waiver fails, and the waiver list may only shrink.
func TestEveryCapabilityIsProbedOrWaived(t *testing.T) {
	declared := declaredStores(t)
	var capabilities, probed []string
	for capability := range declared {
		capabilities = append(capabilities, capability)
	}
	for _, p := range probes() {
		probed = append(probed, p.capability)
	}
	if os.Getenv(updateWaiversEnv) != "" {
		var unprobed []string
		for _, capability := range capabilities {
			if !slices.Contains(probed, capability) {
				unprobed = append(unprobed, capability)
			}
		}
		writeWaivers(t, "probe-waivers.txt",
			"# Capabilities with no recorded-handler probe yet. Remove a line when a probe lands;\n# the list may only shrink (probeWaiverCeiling).", unprobed)
	}
	waived := readWaivers(t, "probe-waivers.txt")
	for _, problem := range audit.CheckWaivers("capability", capabilities, probed, waived, probeWaiverCeiling) {
		t.Error(problem)
	}
}

// censusOwners maps a query package to the capabilities it serves and the
// tables it reads that no capability answers from. The census compares the
// package's SQL literals to the union of its capabilities' declarations plus
// the supporting tables, so a package cannot read an undeclared store by not
// having a probe.
var censusOwners = map[string]struct {
	capabilities []string
	supporting   map[string]string
}{
	"supply/chain/impact": {
		capabilities: []string{
			"supply_chain.impact_findings.list",
			"supply_chain.impact_findings.aggregate",
			"supply_chain.impact_explanation.read",
		},
		supporting: supportingImpactTables(),
	},
	"supply/chain/advisory": {
		capabilities: []string{"supply_chain.advisory_evidence.list", "supply_chain.advisory_catalog.list"},
	},
	"supply/chain/alerts": {
		capabilities: []string{
			"supply_chain.security_alert_reconciliations.list",
			"supply_chain.security_alert_reconciliations.aggregate",
		},
	},
}

func supportingImpactTables() map[string]string {
	const authority = "protected source-vector authority join in the closed derived branch; no probe reaches it live (finding authority opens with #9468)"
	out := map[string]string{}
	for _, table := range []string{
		"eshu_security.role_policy_state", "tenant_scope_grants", "tenants", "workspaces",
		"scanner_advisory_generation_facts", "scanner_advisory_generation_receipts", "scanner_advisory_private_bindings",
		"scanner_advisory_producer_grants", "scanner_advisory_public_activations", "scanner_advisory_public_generations",
		"scanner_advisory_public_registrations", "scanner_authority_epochs", "scanner_image_result_publications",
		"scanner_image_result_reuse_receipts", "scanner_image_scan_fact_associations", "scanner_image_scan_results",
		"scanner_image_target_attempts", "scanner_image_target_sources",
		"scanner_running_image_active_scopes", "scanner_running_image_generations", "scanner_running_image_observations",
		"scanner_running_image_producer_grants", "scanner_static_impact_finding_authority_revisions",
		"scanner_static_impact_public_advisory_evidence",
	} {
		out[audit.ReadTablePrefix+table] = authority
	}
	return out
}

func census(t *testing.T) map[string]audit.Used {
	t.Helper()
	known, err := audit.KnownRelations(filepath.Join(repoRoot(t), "go", "internal", "storage", "postgres"))
	if err != nil {
		t.Fatalf("KnownRelations: %v", err)
	}
	out, err := audit.Census(filepath.Join(repoRoot(t), "go", "internal", "query"), known)
	if err != nil {
		t.Fatalf("Census: %v", err)
	}
	return out
}

func censusViolations(used audit.Used, declared []string, supporting map[string]string) []audit.Violation {
	allowed := slices.Clone(declared)
	for token := range supporting {
		allowed = append(allowed, token)
	}
	return audit.Undeclared(allowed, used)
}

func TestCensusOwnedPackagesStayInsideTheirDeclarations(t *testing.T) {
	declared := declaredStores(t)
	found := census(t)
	for pkg, owner := range censusOwners {
		var stores []string
		for _, capability := range owner.capabilities {
			capabilityStores, ok := declared[capability]
			if !ok {
				t.Fatalf("census owner %s names capability %q, which is not in the matrix", pkg, capability)
			}
			stores = append(stores, capabilityStores...)
		}
		used, ok := found[pkg]
		if !ok {
			t.Errorf("census found no SQL in owned package %s: the owner entry is stale", pkg)
			continue
		}
		for _, violation := range censusViolations(used, stores, owner.supporting) {
			t.Errorf("package %s reads %s outside the stores its capabilities declare %v", pkg, violation, stores)
		}
		for token := range owner.supporting {
			if _, ok := used.Stores[token]; !ok {
				t.Errorf("package %s lists %s as supporting but no longer reads it: delete the entry", pkg, token)
			}
		}
	}
}

// TestCensusUnownedPackagesAreWaived requires every query package that reads a
// store to be either owned above or named in the census waiver list, and keeps
// that list from growing or going stale.
func TestCensusUnownedPackagesAreWaived(t *testing.T) {
	found := census(t)
	var readers, owned []string
	for pkg := range found {
		readers = append(readers, pkg)
		if _, ok := censusOwners[pkg]; ok {
			owned = append(owned, pkg)
		}
	}
	if os.Getenv(updateWaiversEnv) != "" {
		var unowned []string
		for _, pkg := range readers {
			if !slices.Contains(owned, pkg) {
				unowned = append(unowned, pkg)
			}
		}
		writeWaivers(t, "census-waivers.txt",
			"# Query packages that read a store and have no census owner yet (package path under go/internal/query;\n# \".\" is the root package). Own a package in audit_test.go and delete its line; the list may only shrink (censusWaiverCeiling).", unowned)
	}
	waived := readWaivers(t, "census-waivers.txt")
	for _, problem := range audit.CheckWaivers("package", readers, owned, waived, censusWaiverCeiling) {
		t.Error(problem)
	}
}

// TestCensusFailsAPackageThatReadsAnUndeclaredStore is the seeded RED/GREEN
// pair for the static half: a fixture package whose SQL reads a reducer kind
// fails under a graph-only declaration and passes under the declared one.
func TestCensusFailsAPackageThatReadsAnUndeclaredStore(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "fixture")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`package fixture

const kind = "reducer_supply_chain_impact_finding"

const query = ` + "`" + `
SELECT fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id
WHERE fact.fact_kind = $1
` + "`" + `
`)
	if err := os.WriteFile(filepath.Join(dir, "store.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	found, err := audit.Census(root, map[string]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	used, ok := found["fixture"]
	if !ok {
		t.Fatalf("census missed the fixture package: %v", found)
	}
	if got := censusViolations(used, []string{audit.StoreGraph}, nil); len(got) == 0 {
		t.Fatal("RED: a package reading fact_records with a reducer kind passed under a graph-only declaration")
	}
	if got := censusViolations(used, []string{audit.StoreFactRecordsReducer}, nil); len(got) != 0 {
		t.Fatalf("GREEN: declared reducer read still flagged: %v", got)
	}
}
