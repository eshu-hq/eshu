// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"
)

// SupplyChainImpactRows is the row cursor the write transaction's reads return.
type SupplyChainImpactRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

// contentDigestProvenanceKey is the open provenance key a finding row carries
// its content digest under. The digest lets the next pass tell a rewrite that
// changed nothing from one that did, without a column or a schema change.
const contentDigestProvenanceKey = "content_digest"

// supplyChainImpactContentDigest hashes everything a finding row asserts except
// what differs on every pass or every rescan without the finding changing: the
// intent id and cause, the generation id, and the source fact ids the finding
// cites (a rescan mints new fact ids for identical records). The suppression
// state is inside it, so an operator copy flipping between accepted_risk and
// advisory_retracted is a change. An error while marshalling returns the empty
// digest, which the next pass reads as "changed".
func supplyChainImpactContentDigest(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) string {
	return contentDigestOf(supplyChainImpactTypedPayload(write, finding))
}

func contentDigestOf(payload reducerderivedv1.SupplyChainImpactFinding) string {
	payload.IntentID = ""
	payload.Cause = ""
	payload.GenerationID = ""
	payload.EvidenceFactIDs = nil
	if _, has := payload.Provenance[contentDigestProvenanceKey]; has {
		trimmed := make(map[string]any, len(payload.Provenance))
		for key, value := range payload.Provenance {
			if key != contentDigestProvenanceKey {
				trimmed[key] = value
			}
		}
		payload.Provenance = trimmed
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// changedRow is what the writer remembers about a row it is about to upsert.
type changedRow struct {
	factID    string
	stableKey string
	digest    string
	cveID     string
	// advisoryID is the row's advisory id, carried so touchedGroupIDs can
	// name CVE-less groups (#10411). It never joins the CVE-keyed sets.
	advisoryID string
	// members is the advisory-group member id set of the finding (#10408):
	// the alias-connected component the finding's record belongs to. Nil on
	// ungrouped and quarantined findings, which keep per-record keys; the
	// set helpers fall back to the row's own id.
	members []string
	// ident names the row's canonical key for the winners dirty log (#9805).
	ident WinnersDirtyIdentifier
}

// priorGenerationSupplyChainImpactSQL selects the scope's previous generation:
// the most recently activated generation other than the one being written that
// is or was active. A finding's fact id embeds its generation, so the first pass
// of a new generation finds none of its own rows; the previous generation, matched
// by stable key, is the baseline that tells a rescan which derived the same
// findings from one that changed something.
const priorGenerationSupplyChainImpactSQL = `(
    SELECT generation.generation_id
    FROM scope_generations AS generation
    WHERE generation.scope_id = $1
      AND generation.generation_id <> $2
      AND generation.status IN ('active', 'superseded')
    ORDER BY generation.activated_at DESC NULLS LAST, generation.ingested_at DESC
    LIMIT 1
)`

// listStoredSupplyChainImpactFindingsQuery reads the active finding rows of the
// generation being written and of the scope's previous generation: the rows the
// pass may rewrite unchanged, the rows its retraction may tombstone, and the
// baseline of a first pass. Tombstoned rows are deliberately absent, so a row a
// pass derives again after a retraction looks new and counts as changed. No
// array parameter: a generic plan turns a large keep set into a linear scan (see
// retractSupersededSupplyChainImpactFindingsQuery). The fact kind is a literal,
// not a parameter, so the planner can prove the predicate of the partial finding
// indexes under a generic plan too. Arguments: $1 scope id, $2 generation id.
const listStoredSupplyChainImpactFindingsQuery = `
SELECT fact_id,
       generation_id,
       stable_fact_key,
       COALESCE(payload #>> '{provenance,content_digest}', ''),
       COALESCE(payload ->> 'cve_id', ''),
       COALESCE(payload ->> 'advisory_id', ''),
       fencing_token,
       COALESCE(payload #>> '{provenance,advisory_group_members}', '[]')
FROM fact_records
WHERE fact_kind = '` + supplyChainImpactFactKind + `'
  AND scope_id = $1
  AND is_tombstone = FALSE
  AND generation_id IN ($2, ` + priorGenerationSupplyChainImpactSQL + `)
`

// listSupplyChainImpactCVEIDsQuery lists every CVE id the finding rows of the
// generation being written and of the previous generation name, tombstoned rows
// included, plus each row's advisory group member set (#10408). It runs only
// for a recovery write: a retry cannot know what the attempt that died
// retracted or what the new generation dropped. Arguments: $1 scope id,
// $2 generation id.
const listSupplyChainImpactCVEIDsQuery = `
SELECT DISTINCT COALESCE(payload ->> 'cve_id', ''),
       COALESCE(payload #>> '{provenance,advisory_group_members}', '[]')
FROM fact_records
WHERE fact_kind = '` + supplyChainImpactFactKind + `'
  AND scope_id = $1
  AND generation_id IN ($2, ` + priorGenerationSupplyChainImpactSQL + `)
`

type storedFinding struct {
	factID       string
	generationID string
	stableKey    string
	digest       string
	cveID        string
	// advisoryID is the stored row's advisory id, read so a retracted
	// CVE-less row still names the advisory group it touched (#10411).
	advisoryID   string
	fencingToken int64
	// members is the advisory-group member id set the stored row's
	// provenance carries (#10408). Nil when the finding was ungrouped.
	members []string
}

// readStoredSupplyChainImpactFindings returns the active rows of the generation
// being written and of the scope's previous generation.
func readStoredSupplyChainImpactFindings(
	ctx context.Context,
	tx SupplyChainImpactTx,
	scopeID string,
	generationID string,
) ([]storedFinding, error) {
	rows, err := tx.QueryContext(ctx, listStoredSupplyChainImpactFindingsQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("read stored supply chain impact findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var stored []storedFinding
	for rows.Next() {
		var row storedFinding
		var membersRaw string
		if err := rows.Scan(&row.factID, &row.generationID, &row.stableKey, &row.digest, &row.cveID, &row.advisoryID, &row.fencingToken, &membersRaw); err != nil {
			return nil, fmt.Errorf("scan stored supply chain impact finding: %w", err)
		}
		members, err := parseAdvisoryGroupMembers(membersRaw)
		if err != nil {
			return nil, fmt.Errorf("decode stored supply chain impact finding members: %w", err)
		}
		row.members = members
		stored = append(stored, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stored supply chain impact findings: %w", err)
	}
	return stored, nil
}

// changedCVEIDs returns the sorted CVE ids a pass changed, unioned with the
// advisory-group member ids of the rows that changed (#10408): a merge or
// split must reopen the holders of every member, not only the surviving key.
//
// A rerun of a generation that already has rows of its own is judged against
// them, matched by fact id: a row is changed when it is new, revived, or differs
// in content, and a stored row the retraction will tombstone (not kept, not owned
// by a fresher fencing token) is changed. The first pass of a generation has no
// rows of its own, so it is judged against the previous generation's rows,
// matched by stable key: identical content is unchanged, and a finding the
// previous generation had and this one lacks is changed. retract is false for a
// partial-evidence pass, which retracts nothing and proves no removal.
func changedCVEIDs(rows []changedRow, stored []storedFinding, generationID string, fencingToken int64, retract bool) []string {
	own := map[string]storedFinding{}
	prior := map[string]storedFinding{}
	for _, row := range stored {
		if row.generationID == generationID {
			own[row.factID] = row
		} else {
			prior[row.stableKey] = row
		}
	}
	rerun := len(own) > 0
	changed := map[string]struct{}{}
	keepIDs := make(map[string]struct{}, len(rows))
	keepKeys := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		keepIDs[row.factID] = struct{}{}
		keepKeys[row.stableKey] = struct{}{}
		var (
			base storedFinding
			ok   bool
		)
		if rerun {
			base, ok = own[row.factID]
		} else {
			base, ok = prior[row.stableKey]
		}
		if ok && base.digest != "" && base.digest == row.digest {
			continue
		}
		addMemberIDs(changed, row.cveID, row.members)
		if ok {
			// A rewritten row's old member set rides along: a holder
			// keyed by a departed member still needs reopening (#10408).
			addMemberIDs(changed, base.cveID, base.members)
		}
	}
	if retract {
		if rerun {
			for factID, row := range own {
				if _, kept := keepIDs[factID]; kept || row.fencingToken > fencingToken {
					continue
				}
				addMemberIDs(changed, row.cveID, row.members)
			}
		} else {
			for key, row := range prior {
				if _, kept := keepKeys[key]; kept {
					continue
				}
				addMemberIDs(changed, row.cveID, row.members)
			}
		}
	}
	return sortedSetKeys(changed)
}

// derivedCVEIDs returns the sorted CVE ids of every row a pass wrote plus
// every row its retraction will tombstone (#9740). The written set is the
// rows about to be upserted; the retracted set mirrors changedCVEIDs' keep
// logic AND the retraction statement's scope predicate, so the fan-out's
// disjoint compare sees exactly the rows the pass touched. It
// over-approximates only where the retraction itself is fenced: a stored row
// a fresher token protects is still reported, and the pass reruns instead of
// risking a missed reopen. That is the safe direction. A scoped pass reports
// inside its stamp only: rows outside it are never tombstoned.
func derivedCVEIDs(rows []changedRow, stored []storedFinding, generationID string, fencingToken int64, retract bool, scoped []string) []string {
	own := map[string]storedFinding{}
	prior := map[string]storedFinding{}
	for _, row := range stored {
		if row.generationID == generationID {
			own[row.factID] = row
		} else {
			prior[row.stableKey] = row
		}
	}
	rerun := len(own) > 0
	inScope := func(cveID string) bool {
		if len(scoped) == 0 {
			return true
		}
		for _, id := range scoped {
			if id == cveID {
				return true
			}
		}
		return false
	}
	derived := map[string]struct{}{}
	keepIDs := make(map[string]struct{}, len(rows))
	keepKeys := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		keepIDs[row.factID] = struct{}{}
		keepKeys[row.stableKey] = struct{}{}
		if row.cveID != "" {
			derived[row.cveID] = struct{}{}
		}
	}
	if retract {
		if rerun {
			for factID, row := range own {
				if _, kept := keepIDs[factID]; kept || row.fencingToken > fencingToken || row.cveID == "" {
					continue
				}
				if inScope(row.cveID) {
					derived[row.cveID] = struct{}{}
				}
			}
		} else {
			for key, row := range prior {
				if _, kept := keepKeys[key]; kept || row.cveID == "" {
					continue
				}
				if inScope(row.cveID) {
					derived[row.cveID] = struct{}{}
				}
			}
		}
	}
	return sortedSetKeys(derived)
}

// coalescedFindingGroupID returns the advisory-group id (#10411) a finding
// row belongs to: its CVE id when it names one, else its advisory id, each
// trimmed. An empty result names no group.
func coalescedFindingGroupID(cveID string, advisoryID string) string {
	if id := strings.TrimSpace(cveID); id != "" {
		return id
	}
	return strings.TrimSpace(advisoryID)
}

// touchedGroupIDs returns the sorted advisory-group ids a pass changed
// (#10411), keyed by coalescedFindingGroupID so CVE-less groups are named
// by their advisory id. The change judgement mirrors changedCVEIDs exactly
// — new, revived, or content-different rows plus the stored rows the
// retraction tombstones — with one addition: a changed row that replaces a
// stored row of a different group touches the old group too, because the
// graph projection must also reconcile the group the row left. The
// CVE-keyed sets are untouched by this function.
func touchedGroupIDs(rows []changedRow, stored []storedFinding, generationID string, fencingToken int64, retract bool) []string {
	own := map[string]storedFinding{}
	prior := map[string]storedFinding{}
	for _, row := range stored {
		if row.generationID == generationID {
			own[row.factID] = row
		} else {
			prior[row.stableKey] = row
		}
	}
	rerun := len(own) > 0
	touched := map[string]struct{}{}
	add := func(cveID string, advisoryID string) {
		if id := coalescedFindingGroupID(cveID, advisoryID); id != "" {
			touched[id] = struct{}{}
		}
	}
	keepIDs := make(map[string]struct{}, len(rows))
	keepKeys := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		keepIDs[row.factID] = struct{}{}
		keepKeys[row.stableKey] = struct{}{}
		var (
			base storedFinding
			ok   bool
		)
		if rerun {
			base, ok = own[row.factID]
		} else {
			base, ok = prior[row.stableKey]
		}
		if ok && base.digest != "" && base.digest == row.digest {
			continue
		}
		add(row.cveID, row.advisoryID)
		if ok {
			add(base.cveID, base.advisoryID)
		}
	}
	if retract {
		if rerun {
			for factID, row := range own {
				if _, kept := keepIDs[factID]; kept || row.fencingToken > fencingToken {
					continue
				}
				add(row.cveID, row.advisoryID)
			}
		} else {
			for key, row := range prior {
				if _, kept := keepKeys[key]; kept {
					continue
				}
				add(row.cveID, row.advisoryID)
			}
		}
	}
	return sortedSetKeys(touched)
}

// readSupplyChainImpactCVEIDs returns every CVE id the (scope, generation)'s
// finding rows name, sorted, unioned with the rows' advisory-group member
// sets (#10408).
func readSupplyChainImpactCVEIDs(ctx context.Context, tx SupplyChainImpactTx, scopeID, generationID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, listSupplyChainImpactCVEIDsQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("read supply chain impact recovery cve ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := map[string]struct{}{}
	for rows.Next() {
		var id, membersRaw string
		if err := rows.Scan(&id, &membersRaw); err != nil {
			return nil, fmt.Errorf("scan supply chain impact recovery cve id: %w", err)
		}
		if id != "" {
			ids[id] = struct{}{}
		}
		members, err := parseAdvisoryGroupMembers(membersRaw)
		if err != nil {
			return nil, fmt.Errorf("decode supply chain impact recovery members: %w", err)
		}
		for _, member := range members {
			ids[member] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate supply chain impact recovery cve ids: %w", err)
	}
	return sortedSetKeys(ids), nil
}

// listSupplyChainImpactGroupIDsQuery lists the (cve_id, advisory_id) pair
// of every finding row of the generation being written and of the previous
// generation, tombstoned rows included. It is the recovery companion of
// listSupplyChainImpactCVEIDsQuery for the advisory-group projection
// (#10411): a retry whose fan-out died with the previous attempt owes a
// projection for every group the scope's rows name, including CVE-less
// groups the CVE-only recovery read cannot name. Arguments: $1 scope id,
// $2 generation id.
const listSupplyChainImpactGroupIDsQuery = `
SELECT DISTINCT COALESCE(payload ->> 'cve_id', ''), COALESCE(payload ->> 'advisory_id', '')
FROM fact_records
WHERE fact_kind = '` + supplyChainImpactFactKind + `'
  AND scope_id = $1
  AND generation_id IN ($2, ` + priorGenerationSupplyChainImpactSQL + `)
`

// readSupplyChainImpactGroupIDs returns every coalesced advisory-group id
// the (scope, generation)'s finding rows name, sorted.
func readSupplyChainImpactGroupIDs(ctx context.Context, tx SupplyChainImpactTx, scopeID, generationID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, listSupplyChainImpactGroupIDsQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("read supply chain impact recovery group ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := map[string]struct{}{}
	for rows.Next() {
		var cveID, advisoryID string
		if err := rows.Scan(&cveID, &advisoryID); err != nil {
			return nil, fmt.Errorf("scan supply chain impact recovery group id: %w", err)
		}
		if id := coalescedFindingGroupID(cveID, advisoryID); id != "" {
			ids[id] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate supply chain impact recovery group ids: %w", err)
	}
	return sortedSetKeys(ids), nil
}

func sortedSetKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// parseAdvisoryGroupMembers decodes the advisory-group member set the
// stored-finding and recovery selects project from
// payload.provenance.advisory_group_members (#10408). A missing, null, or
// empty array reads as nil. Malformed JSON fails the pass so a corrupt row
// can never silently narrow the fan-out.
func parseAdvisoryGroupMembers(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "[]" || trimmed == "null" {
		return nil, nil
	}
	var members []string
	if err := json.Unmarshal([]byte(trimmed), &members); err != nil {
		return nil, fmt.Errorf("decode advisory group members: %w", err)
	}
	out := make([]string, 0, len(members))
	for _, member := range members {
		if member = strings.TrimSpace(member); member != "" {
			out = append(out, member)
		}
	}
	return out, nil
}

// addMemberIDs contributes a row's group id and advisory-group member ids to
// a set (#10408), blanks dropped. A member-less row falls back to its
// singleton id, so ungrouped findings keep per-record keys.
func addMemberIDs(set map[string]struct{}, cveID string, members []string) {
	if id := strings.TrimSpace(cveID); id != "" {
		set[id] = struct{}{}
	}
	for _, member := range members {
		if id := strings.TrimSpace(member); id != "" {
			set[id] = struct{}{}
		}
	}
}

// touchedGroupIDs is defined once above (#10411): the pass's changed
// advisory-group ids keyed by coalesced CVE/advisory id. Member ids for
// holder lookup ride changedCVEIDs and the recovery set via addMemberIDs
// (#10408), never the touched set, so the projection contract stays exact.
