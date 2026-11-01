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
       fencing_token
FROM fact_records
WHERE fact_kind = '` + supplyChainImpactFactKind + `'
  AND scope_id = $1
  AND is_tombstone = FALSE
  AND generation_id IN ($2, ` + priorGenerationSupplyChainImpactSQL + `)
`

// listSupplyChainImpactCVEIDsQuery lists every CVE id the finding rows of the
// generation being written and of the previous generation name, tombstoned rows
// included. It runs only for a recovery write: a retry cannot know what the
// attempt that died retracted or what the new generation dropped. Arguments:
// $1 scope id, $2 generation id.
const listSupplyChainImpactCVEIDsQuery = `
SELECT DISTINCT COALESCE(payload ->> 'cve_id', '')
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
	fencingToken int64
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
		if err := rows.Scan(&row.factID, &row.generationID, &row.stableKey, &row.digest, &row.cveID, &row.fencingToken); err != nil {
			return nil, fmt.Errorf("scan stored supply chain impact finding: %w", err)
		}
		stored = append(stored, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stored supply chain impact findings: %w", err)
	}
	return stored, nil
}

// changedCVEIDs returns the sorted CVE ids a pass changed.
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
		if row.cveID != "" {
			changed[row.cveID] = struct{}{}
		}
	}
	if retract {
		if rerun {
			for factID, row := range own {
				if _, kept := keepIDs[factID]; kept || row.fencingToken > fencingToken || row.cveID == "" {
					continue
				}
				changed[row.cveID] = struct{}{}
			}
		} else {
			for key, row := range prior {
				if _, kept := keepKeys[key]; kept || row.cveID == "" {
					continue
				}
				changed[row.cveID] = struct{}{}
			}
		}
	}
	return sortedSetKeys(changed)
}

// readSupplyChainImpactCVEIDs returns every CVE id the (scope, generation)'s
// finding rows name, sorted.
func readSupplyChainImpactCVEIDs(ctx context.Context, tx SupplyChainImpactTx, scopeID, generationID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, listSupplyChainImpactCVEIDsQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("read supply chain impact recovery cve ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan supply chain impact recovery cve id: %w", err)
		}
		if id != "" {
			ids[id] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate supply chain impact recovery cve ids: %w", err)
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
