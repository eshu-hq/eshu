// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The #10019 acceptance fixture: an OS finding keyed by the UPSTREAM CVE,
// not by the distro record id. The holder scope carries the distro record
// (whose cve_id is the distro id and whose upstream list names the CVE) and
// its affected package; the installed curl row lives in a scan scope and
// reaches the holder through the cross-scope advisory-target reader. The
// operator scope carries only a cve_id-scoped suppression, which the
// operator pass cannot turn into a finding row: no affected package carries
// the upstream id, so the operator writes nothing and ChangedCVEIDs is
// empty without the suppression diff.
const (
	supCVEID    = "CVE-2023-38039"
	supDistroID = "DEBIAN-CVE-2023-38039"
	supHolder   = "scope:10019:os-holder"
	supScan     = "scope:10019:scan"
	supOpScope  = "operator:vulnerability_suppressions"
	supCurlFix  = "7.88.1-10+deb12u3"
	supCurlVuln = "7.88.1-10+deb12u2"
)

func seedSupScanScope(t *testing.T, c *convergence) {
	t.Helper()
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'container_image', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb)`, supScan, now); err != nil {
		t.Fatalf("insert scan scope: %v", err)
	}
	gen := fanOutGen(supScan, "g1")
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, gen, supScan, now); err != nil {
		t.Fatalf("insert scan generation: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, supScan, gen); err != nil {
		t.Fatalf("activate scan generation: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload, fencing_token)
VALUES ('os-package:10019:curl', $1, $2, 'vulnerability.os_package', 'os-package:10019:curl',
  'synthetic', 'os-package:10019:curl', $3, $3, FALSE,
  jsonb_build_object(
    'distro', 'debian', 'distro_version', '12', 'package_manager', 'dpkg',
    'name', 'curl', 'arch', 'amd64',
    'repository_class', 'vendor', 'vendor_advisory_source', 'debian',
    'installed_version_raw', $4::text,
    'purl', 'pkg:deb/debian/curl@' || $4::text || '?arch=amd64'), 0)`,
		supScan, gen, now, supCurlVuln); err != nil {
		t.Fatalf("insert os_package row: %v", err)
	}
}

// seedSupHolder seeds the holder's own advisory evidence: the distro record
// (cve_id is the distro id; the upstream CVE lives only in its upstream
// list) and the affected package for curl on bookworm below the fix.
func seedSupHolder(t *testing.T, c *convergence) {
	t.Helper()
	fanOutSeedScope(t, c.ctx, c.db, supHolder, "g1")
	gen := fanOutGen(supHolder, "g1")
	c.upsert(
		c.envelope(supHolder, gen, "fact:10019:record", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": supDistroID, "cve_id": supDistroID, "source": "osv",
			"upstream":     []string{supCVEID},
			"published_at": "2023-09-15T04:15:10Z",
		}),
		c.envelope(supHolder, gen, "fact:10019:affected", chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": supDistroID, "cve_id": supDistroID,
			"package_id": "os://debian/curl", "package_name": "curl", "ecosystem": "os",
			"purl":           "pkg:deb/debian/curl?arch=source&distro=bookworm",
			"fixed_versions": []string{supCurlFix},
			"affected_ranges": []any{map[string]any{
				"events":   []any{map[string]any{"introduced": "0"}, map[string]any{"fixed": supCurlFix}},
				"releases": []string{"debian:12"}, "type": "ECOSYSTEM",
			}},
			"source": "osv",
		}),
	)
	fanOutSeedItem(t, c.ctx, c.db, supHolder, "g1")
}

// seedSupOperatorMutation posts a suppression mutation through the real
// endpoint and returns its generation without activating it or seeding an
// item. A repeat post with the same suppression id edits it (a re-scope or
// a new reason) and mints the next generation.
func seedSupOperatorMutation(t *testing.T, c *convergence, suppressionID, cveID, reason string) string {
	t.Helper()
	handler := &query.SupplyChainHandler{SuppressionMutations: query.NewPostgresVulnerabilitySuppressionMutationStore(c.db)}
	mux := http.NewServeMux()
	handler.Mount(mux)
	body := `{"suppression_id":"` + suppressionID + `","justification":"accepted_risk","authored_at":"2026-10-31T11:30:00Z",
		"reason":` + strconv.Quote(reason) + `,"evidence_ref":"https://registry.example.com/evidence/10019","scope":{"cve_id":"` + cveID + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/supply-chain/impact/suppressions", strings.NewReader(body))
	req = req.WithContext(query.ContextWithAuthContext(req.Context(), query.AuthContext{
		Mode: query.AuthModeShared, SubjectClass: "shared_token", SubjectIDHash: "sha256:synthetic-operator", AllScopes: true,
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("post cve_id suppression status = %d body = %s", rec.Code, rec.Body.String())
	}
	var response query.VulnerabilitySuppressionMutationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode suppression response: %v", err)
	}
	return response.GenerationID
}

// seedSupOperatorCVEID creates an accepted_risk suppression scoped by
// cve_id alone through the real mutation endpoint, like seedOperator does
// for the advisory scope: mutation, activation, and queue item.
func seedSupOperatorCVEID(t *testing.T, c *convergence) string {
	t.Helper()
	gen := seedSupOperatorMutation(t, c, "suppression-10019-cveid", supCVEID, "compensating control")
	activateAdvisorySuppressionGeneration(t, c.ctx, c.db, gen)
	fanOutSeedItemAt(t, c.ctx, c.db, supOpScope, gen)
	return gen
}

// supCopies returns each scope's active finding suppression state for the CVE.
func supCopies(t *testing.T, c *convergence) map[string]string {
	t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT f.scope_id, COALESCE(f.payload->>'suppression_state','') FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone AND f.payload->>'cve_id' = $1`, supCVEID)
	if err != nil {
		t.Fatalf("read copies: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var scope, state string
		if err := rows.Scan(&scope, &state); err != nil {
			t.Fatal(err)
		}
		out[scope] = state
	}
	return out
}

// supAdvisory returns the holder finding's advisory_id for the CVE: the
// distro record that decided it. It pins the OS keying (cve_id is the
// upstream CVE while advisory_id is the distro record).
func supAdvisory(t *testing.T, c *convergence) string {
	t.Helper()
	var advisory string
	if err := c.db.QueryRowContext(c.ctx, `
SELECT f.payload->>'advisory_id' FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.scope_id = $1 AND f.payload->>'cve_id' = $2`, supHolder, supCVEID).Scan(&advisory); err != nil {
		t.Fatalf("read holder advisory: %v", err)
	}
	return advisory
}

func countSupOperatorRows(t *testing.T, c *convergence) int {
	t.Helper()
	var n int
	if err := c.db.QueryRowContext(c.ctx, `
SELECT count(*) FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone AND f.scope_id = $1`, supOpScope).Scan(&n); err != nil {
		t.Fatalf("count operator rows: %v", err)
	}
	return n
}

// TestSuppressionFanOutCVEIDOnlyScopeReopensOSHolderLive is the #10019
// criterion-1 acceptance regression, RED first: the OS finding is keyed by
// the upstream CVE, the suppression is scoped cve_id-only, and the operator
// pass writes no row of its own. After the operator pass and the holder
// fan-out the holder reads accepted_risk, where it read active before.
func TestSuppressionFanOutCVEIDOnlyScopeReopensOSHolderLive(t *testing.T) {
	c := newConvergence(t, false)
	seedSupScanScope(t, c)
	seedSupHolder(t, c)
	c.drain()

	before := supCopies(t, c)
	if before[supHolder] != string(supplychaincore.SupplyChainSuppressionStateActive) {
		t.Fatalf("holder state before the suppression = %q, want active (all copies %v, handled %v)", before[supHolder], before, c.handled)
	}
	if got := supAdvisory(t, c); got != supDistroID {
		t.Fatalf("holder finding advisory_id = %q, want %q: the finding must be the OS record keyed by the upstream CVE", got, supDistroID)
	}

	seedSupOperatorCVEID(t, c)
	c.drain()

	if got := countSupOperatorRows(t, c); got != 0 {
		t.Fatalf("operator finding rows = %d, want none: the cve_id-only pass must write no row of its own", got)
	}
	after := supCopies(t, c)
	if after[supHolder] != "accepted_risk" {
		t.Fatalf("holder state after the operator pass and fan-out = %q, want accepted_risk (all copies %v, handled %v)", after[supHolder], after, c.handled)
	}
}

// supConvergence rebuilds the criterion-1 end state: an OS holder reading
// active, then a cve_id suppression converging it to accepted_risk through
// the diff and fan-out.
func supConvergence(t *testing.T) *convergence {
	t.Helper()
	c := newConvergence(t, false)
	seedSupScanScope(t, c)
	seedSupHolder(t, c)
	c.drain()
	if got := supCopies(t, c)[supHolder]; got != string(supplychaincore.SupplyChainSuppressionStateActive) {
		t.Fatalf("holder state before the suppression = %q, want active", got)
	}
	seedSupOperatorCVEID(t, c)
	c.drain()
	if got := supCopies(t, c)[supHolder]; got != "accepted_risk" {
		t.Fatalf("holder state after the fan-out = %q, want accepted_risk", got)
	}
	return c
}

// TestSuppressionUnchangedSetReopensNobodyLive is the #10019 criterion-2
// proof: rerunning the operator generation after its change fanned out
// reopens no holder. The duplicate pass admits (its own admission row is
// already there), names no suppression id, and the drain handles only the
// operator rerun itself.
func TestSuppressionUnchangedSetReopensNobodyLive(t *testing.T) {
	c := supConvergence(t)
	before := append([]string(nil), c.handled...)
	c.rerun(supOpScope)
	c.drain()
	if len(c.handled) != len(before)+1 {
		t.Fatalf("handled %d passes after the rerun, want exactly the operator rerun (before %v, after %v)",
			len(c.handled)-len(before), before, c.handled)
	}
	last := c.handled[len(c.handled)-1]
	if !strings.HasPrefix(last, supOpScope+"#") {
		t.Fatalf("last handled = %q, want the operator rerun and no holder pass", last)
	}
	if got := supCopies(t, c)[supHolder]; got != "accepted_risk" {
		t.Fatalf("holder state after the rerun = %q, want accepted_risk", got)
	}
}

// TestSuppressionDiffRetryRepeatsTheSetAfterHolderReadFailureLive is the
// at-least-once proof for the suppression path: the first holder read fails
// after the writer committed, the item is retried, the rewrite changes
// nothing, and the retry's diff repeats the same set, so the holder still
// converges.
func TestSuppressionDiffRetryRepeatsTheSetAfterHolderReadFailureLive(t *testing.T) {
	c := newConvergence(t, false)
	seedSupScanScope(t, c)
	seedSupHolder(t, c)
	c.drain()
	flaky := &flakyFindFanOut{SupplyChainImpactFanOut: c.fanOut}
	c.handler.FanOut = flaky
	seedSupOperatorCVEID(t, c)
	c.drain()
	if !flaky.failed.Load() {
		t.Fatal("the injected holder read failure never fired")
	}
	if got := supCopies(t, c)[supHolder]; got != "accepted_risk" {
		t.Fatalf("holder state after the retry = %q, want accepted_risk (handled %v)", got, c.handled)
	}
}
